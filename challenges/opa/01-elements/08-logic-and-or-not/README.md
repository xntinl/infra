# 8. Logic: AND, OR, and NOT

## Prerequisites

- OPA installed and working (exercise 01)
- Understanding of rules and `default` (exercises 05-06)
- Familiarity with `some` and iteration (exercise 07)

## Learning Objectives

After completing this exercise, you will be able to:

- Express AND logic using multiple conditions in a rule body
- Express OR logic using multiple rules with the same name
- Express NOT logic using the `not` keyword with helper rules
- Combine AND, OR, and NOT to build a real-world RBAC policy

## Why Logic Composition Matters

If you come from any imperative language, you will look for `&&`, `||`, and `!`. Rego does not have them. Logic is expressed through the **structure** of the rules, not through operators.

Once you understand this, Rego policies read like plain English sentences. This exercise ties together everything from the previous seven exercises into a realistic authorization scenario.

## AND = Multiple Lines in the Body

When you put multiple conditions in the body of a rule, **all** of them must be satisfied. It is an implicit AND:

```rego
allow if {
    input.role == "admin"    # condition 1
    input.active == true     # AND condition 2
}
```

This says: "allow is true IF role is admin **AND** active is true". If either fails, the entire rule does not match.

## OR = Multiple Rules with the Same Name

When you define multiple rules with the same name, it is enough for **one** to be true. It is an OR:

```rego
allow if {
    input.role == "admin"
}

allow if {
    input.role == "viewer"
    input.action == "read"
}
```

This says: "allow is true IF role is admin, **OR** IF (role is viewer AND action is read)".

## NOT = The `not` Keyword

`not` negates a condition or a helper rule:

```rego
allow if {
    input.role == "viewer"
    not is_restricted
}
```

This says: "allow is true IF role is viewer **AND** the resource is NOT restricted".

## Building a Policy That Combines All Three

Now build a mini-RBAC policy that uses AND, OR, and NOT together. Create a file called `logic.rego`:

```rego
package logic

import rego.v1

default allow := false

# --- AND ---
# An active admin can do anything
allow if {
    input.role == "admin"
    input.active == true
}

# --- OR ---
# An editor can read articles
allow if {
    input.role == "editor"
    input.action == "read"
    input.resource == "articles"
}

# An editor can write articles (OR -- another allow rule)
allow if {
    input.role == "editor"
    input.action == "write"
    input.resource == "articles"
}

# A viewer can read any resource (OR -- another allow rule)
allow if {
    input.role == "viewer"
    input.action == "read"
}

# --- NOT ---
# Restricted resources
restricted_resources := {"secrets", "credentials", "keys"}

is_restricted if {
    input.resource in restricted_resources
}

# A viewer can read, BUT NOT restricted resources
allow_safe_read if {
    input.role == "viewer"
    input.action == "read"
    not is_restricted
}
```

Read the `allow` policy as a sentence:

> "allow IF (role=admin AND active=true)
> OR IF (role=editor AND action=read AND resource=articles)
> OR IF (role=editor AND action=write AND resource=articles)
> OR IF (role=viewer AND action=read)"

Each `allow if { ... }` block is an alternative (OR). Inside each block, all lines are AND.

## Testing AND

Create `input-admin.json`:

```json
{
    "role": "admin",
    "active": true,
    "action": "delete",
    "resource": "users"
}
```

```bash
opa eval --format pretty -d logic.rego -i input-admin.json "data.logic.allow"
```

```
true
```

Both conditions are satisfied: role is admin AND active is true.

**Verification:**

Now test an **inactive** admin -- the second condition fails. Change one variable (active) to isolate the effect:

```bash
opa eval --format pretty -d logic.rego -i /dev/stdin "data.logic.allow" <<'EOF'
{"role": "admin", "active": false, "action": "read", "resource": "articles"}
EOF
```

```
false
```

`false` (not undefined) because we have `default allow := false`. The inactive admin does not match the admin rule (fails `active == true`), and does not match the other rules either, so the default kicks in.

## Testing OR

Create `input-viewer.json`:

```json
{
    "role": "viewer",
    "active": true,
    "action": "read",
    "resource": "articles"
}
```

```bash
opa eval --format pretty -d logic.rego -i input-viewer.json "data.logic.allow"
```

```
true
```

This matches the "viewer can read" rule (the fourth definition of `allow`).

Now change the action to "write" -- one variable changed:

Create `input-denied.json`:

```json
{
    "role": "viewer",
    "active": true,
    "action": "write",
    "resource": "articles"
}
```

```bash
opa eval --format pretty -d logic.rego -i input-denied.json "data.logic.allow"
```

```
false
```

A viewer writing does not match any of the 4 `allow` rules. The default kicks in: `false`.

## Testing NOT

The viewer can read articles (not restricted):

```bash
opa eval --format pretty -d logic.rego -i input-viewer.json "data.logic.allow_safe_read"
```

```
true
```

But cannot read secrets (restricted). Change only the resource to isolate the effect:

```bash
opa eval --format pretty -d logic.rego -i /dev/stdin "data.logic.allow_safe_read" <<'EOF'
{"role": "viewer", "action": "read", "resource": "secrets"}
EOF
```

(no output -- undefined)

`allow_safe_read` does not have `default`, so when `not is_restricted` fails (because "secrets" IS in the restricted resources set), the rule is undefined.

You can verify that `is_restricted` detects secrets:

```bash
opa eval --format pretty -d logic.rego -i /dev/stdin "data.logic.is_restricted" <<'EOF'
{"role": "viewer", "action": "read", "resource": "secrets"}
EOF
```

```
true
```

## Visual Summary

```
AND (inside the body):
    allow if {
        condition_1    <- all must
        condition_2    <- be true
        condition_3    <-
    }

OR (multiple rules):
    allow if {         <- it is enough
        condition_A    <- for this to be true
    }
    allow if {         <- OR for this
        condition_B    <- to be true
    }

NOT (negation):
    allow if {
        not condition  <- true if condition is false/undefined
    }
```

## Common Mistakes

A realistic mistake is trying to use `not` directly on an expression instead of a helper rule:

```rego
# WRONG: not with an inline comparison
allow if {
    not input.resource == "secrets"
}
```

While this specific example might work, it can lead to unexpected behavior with `undefined` values. If `input.resource` does not exist at all, `input.resource == "secrets"` is `undefined`, and `not undefined` is `true` -- so the rule would match even when no resource is provided.

The safer pattern is to use a named helper rule:

```rego
# CORRECT: not with a helper rule
is_restricted if {
    input.resource in restricted_resources
}

allow if {
    not is_restricted
}
```

This makes the logic explicit and easier to test independently.

## Verify What You Learned

```bash
opa eval --format pretty -d logic.rego -i input-admin.json "data.logic.allow"
```

```
true
```

```bash
opa eval --format pretty -d logic.rego -i input-viewer.json "data.logic.allow"
```

```
true
```

```bash
opa eval --format pretty -d logic.rego -i input-denied.json "data.logic.allow"
```

```
false
```

```bash
opa eval --format pretty -d logic.rego -i input-viewer.json "data.logic.allow_safe_read"
```

```
true
```

If all 4 produce the expected result, you have completed the elements section.

## Section Summary: 01 - Elements

You have completed all 8 exercises in the Elements section. Here is what you covered:

### Key Concepts Learned

1. **OPA vs Rego** -- OPA is the engine, Rego is the language. Like Node.js and JavaScript.
2. **Data types** -- 4 scalars (string, number, boolean, null) and 3 composites (array, object, set). Empty braces `{}` are an object, not a set.
3. **Operators** -- `:=` assigns, `==` compares. No `&&`, `||`, `!` operators exist. Failed comparisons produce `undefined`, not `false`.
4. **The input document** -- External JSON data passed with `-i`. Read-only. Missing fields are `undefined`, not `null`.
5. **Rules** -- Every `.rego` file has `package`, `import rego.v1`, and rules. Rules produce `true` or a value when conditions match, `undefined` when they do not.
6. **True, false, and undefined** -- Three states, not two. `default` converts `undefined` to an explicit value. Always use `default allow := false` in authorization policies.
7. **Iteration with `some`** -- Declarative iteration: describe what you want, not how to get it. `contains` builds sets of results.
8. **Logic composition** -- AND = multiple lines in a body. OR = multiple rules with the same name. NOT = the `not` keyword with helper rules.

### Exercises Completed

| Exercise | Topic | Key Takeaway |
|---|---|---|
| 01 | Hello OPA | Install OPA, use `opa eval` and `opa run` |
| 02 | Data Types | 7 types, `type_name()`, the `{}` vs `set()` gotcha |
| 03 | Operators | `:=` vs `==`, arithmetic, `in` for membership |
| 04 | Input Document | Pass JSON with `-i`, dot notation, undefined fields |
| 05 | Your First Rule | `package`, `import rego.v1`, boolean and value rules |
| 06 | True/False/Undefined | Three states, `default`, fail-closed security |
| 07 | Iteration with `some` | `some`, `contains`, declarative filtering |
| 08 | Logic AND/OR/NOT | Rule structure as logic, RBAC policy pattern |

### Important Notes to Remember

- **Always use `--format pretty`** unless you need raw JSON output for scripting.
- **Always use `import rego.v1`** to get modern syntax with `if`, `contains`, and `in`.
- **Always use `default` on authorization rules** to ensure fail-closed behavior.
- **`undefined` is not `false`** -- this distinction is the single biggest source of security bugs in OPA policies.
- **Rego is declarative** -- you describe what you want, not how to compute it. Fight the urge to think in loops and conditionals.

## What's Next

You are ready to move on to `02-rego-fundamentals`, which covers more advanced Rego features: comprehensions, functions, testing, and more complex policy patterns.

## Reference

- [Rule Bodies (AND)](https://www.openpolicyagent.org/docs/latest/policy-language/#rule-bodies)
- [Incremental Definitions (OR)](https://www.openpolicyagent.org/docs/latest/policy-language/#incremental-definitions)
- [Negation](https://www.openpolicyagent.org/docs/latest/policy-language/#negation)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- test complex logic patterns interactively
- [OPA Policy Language Guide](https://www.openpolicyagent.org/docs/latest/policy-language/) -- complete language reference
- [Styra Academy](https://academy.styra.com/) -- structured courses for advancing your OPA skills
- [OPA Contrib](https://github.com/open-policy-agent/contrib) -- community-maintained example policies

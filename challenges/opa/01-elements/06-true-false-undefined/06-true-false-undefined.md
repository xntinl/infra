# 6. True, False, and Undefined

## Prerequisites

- OPA installed and working (exercise 01)
- Understanding of rules and the `if` keyword (exercise 05)
- Awareness that unmatched rules produce no output (exercises 03-05)

## Learning Objectives

After completing this exercise, you will be able to:

- Distinguish between the three states a Rego rule can have: `true`, `undefined`, and `false`
- Use `default` to guarantee an explicit `false` when no rule matches
- Explain why `undefined` creates a security risk in authorization policies
- Apply the fail-closed pattern to authorization rules

## Why This Is the Most Important Concept

This is the most important concept in OPA. If you do not understand it, your security policies will have gaps.

In most languages, something is `true` or `false`. Two states. In Rego there are **three**:

| State | Meaning |
|---|---|
| `true` | The rule matched. The conditions were satisfied. |
| `undefined` | No rule matched. There is no result. It is not `true` or `false` -- it is **absence of value**. |
| `false` | Explicitly evaluated to false. Only happens with `default` or explicit assignment. |

The difference between `undefined` and `false` is subtle but critical. You already saw it in previous exercises: when a rule does not match, the output is empty. That is `undefined`.

## Seeing It in Action

The exercise directory contains two files that illustrate the difference.

`without-default.rego`:

```rego
package nodefault

import rego.v1

allow if {
    input.role == "admin"
}

allow if {
    input.role == "editor"
    input.action == "read"
}
```

`with-default.rego`:

```rego
package withdefault

import rego.v1

default allow := false

allow if {
    input.role == "admin"
}

allow if {
    input.role == "editor"
    input.action == "read"
}
```

The only difference is one line: `default allow := false`. That line says: "if no `allow` rule matches, the value of `allow` is `false`".

The directory also contains two input files.

`input-match.json` -- an admin (should match):

```json
{
    "role": "admin",
    "action": "delete"
}
```

`input-nomatch.json` -- a guest (matches no rule):

```json
{
    "role": "guest",
    "action": "read"
}
```

## When the Input Matches: Both Behave the Same

```bash
opa eval --format pretty -d without-default.rego -i input-match.json "data.nodefault.allow"
```

```
true
```

```bash
opa eval --format pretty -d with-default.rego -i input-match.json "data.withdefault.allow"
```

```
true
```

When the input matches a rule, there is no difference. Both produce `true`.

## When the Input Does NOT Match: Here Is the Difference

```bash
opa eval --format pretty -d without-default.rego -i input-nomatch.json "data.nodefault.allow"
```

(no output -- NOTHING)

```bash
opa eval --format pretty -d with-default.rego -i input-nomatch.json "data.withdefault.allow"
```

```
false
```

- **Without default**: no result. `allow` does not exist. It is `undefined`.
- **With default**: there is an explicit result: `false`.

**Verification:**

Run both commands above and compare the outputs. The with-default version always produces a definitive answer.

## Why This Matters: Security

Imagine an API gateway that queries OPA before every request:

```
API Gateway: "Can this user DELETE /users/42?"
```

**With default (fail-closed)**:
1. OPA evaluates the rules
2. None match
3. OPA responds `false`
4. API gateway denies the request
5. **SECURE**

**Without default (fail-open risk)**:
1. OPA evaluates the rules
2. None match
3. OPA responds with nothing (undefined)
4. API gateway receives an empty response
5. API gateway might interpret "no response" as "no restriction"
6. API gateway allows the request
7. **INSECURE**

**Golden rule: always use `default` on authorization rules.** `default allow := false` guarantees that if something is not explicitly permitted, it is denied.

## Exploring in the REPL

```bash
opa run with-default.rego without-default.rego
```

```
> input := {"role": "guest", "action": "read"}

> data.nodefault.allow

> data.withdefault.allow
false

> input := {"role": "admin", "action": "delete"}

> data.nodefault.allow
true

> data.withdefault.allow
true

> exit
```

With the guest:
- `data.nodefault.allow` produces no output (undefined)
- `data.withdefault.allow` produces `false`

With the admin:
- Both produce `true`

## Common Mistakes

A realistic mistake is adding `default` with the wrong syntax. For example:

```rego
default allow = false
```

With `import rego.v1`, the assignment operator for defaults is `:=`, not `=`. Using `=` will cause a parse error. The correct syntax is:

```rego
default allow := false
```

Another mistake is placing `default` after the rule definitions. While OPA does not strictly require a specific order, it is a strong convention to put `default` first -- it makes the fallback value immediately visible to anyone reading the policy.

## Verify What You Learned

Without default and without match -- no output:

```bash
opa eval --format pretty -d without-default.rego -i input-nomatch.json "data.nodefault.allow"
```

(empty output)

With default and without match -- explicit `false`:

```bash
opa eval --format pretty -d with-default.rego -i input-nomatch.json "data.withdefault.allow"
```

```
false
```

With default and with match -- `true`:

```bash
opa eval --format pretty -d with-default.rego -i input-match.json "data.withdefault.allow"
```

```
true
```

## What's Next

You now understand the three-state logic of Rego and why `default` is essential for secure policies. The next exercise introduces iteration with `some` -- how to search through collections without writing loops.

## Reference

- [Default Keyword](https://www.openpolicyagent.org/docs/latest/policy-language/#default-keyword)
- [The Open World Assumption](https://www.openpolicyagent.org/docs/latest/philosophy/#the-open-world-assumption) -- why `undefined` exists in Rego

## Additional Resources

- [OPA Best Practices](https://www.openpolicyagent.org/docs/latest/policy-performance/) -- performance and correctness tips
- [Rego Playground](https://play.openpolicyagent.org/) -- experiment with default behavior interactively

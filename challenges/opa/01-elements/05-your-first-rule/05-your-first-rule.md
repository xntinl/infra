# 5. Your First Rule

## Prerequisites

- OPA installed and working (exercise 01)
- Understanding of data types (exercise 02) and operators (exercise 03)
- Familiarity with the `input` document (exercise 04)

## Learning Objectives

After completing this exercise, you will be able to:

- Structure a `.rego` file with `package`, `import rego.v1`, and rules
- Write boolean rules that produce `true` when conditions are met
- Write value-producing rules that return strings, numbers, or other types
- Combine multiple conditions in a single rule body (implicit AND)

## Why Rules

Up to now you evaluated loose expressions and read input fields. Now you will write your first real policy with rules. This is where you learn the 3 pieces that every `.rego` file has: `package`, `import rego.v1`, and the rules themselves. Rules are the building blocks of every OPA policy.

## The Structure of a `.rego` File

Every `.rego` file has this structure:

```rego
package package_name

import rego.v1

# your rules here
```

- **`package`** declares the namespace. It is like a folder that organizes your rules.
- **`import rego.v1`** activates the modern Rego syntax.

## `package` -- Organizing with Namespaces

The package gives a name to your group of rules. When you evaluate, you access the rules through that name:

```
package myapp           -> rules are accessed as data.myapp.rule_name
package auth.policies   -> rules are accessed as data.auth.policies.rule_name
```

`data` is the root prefix. Then comes the package name. Then the rule name.

## `import rego.v1` -- Modern Syntax

Without this import, Rego uses legacy syntax. With it, keywords like `if`, `contains`, and `in` are activated. Compare:

```rego
# Without import rego.v1 (legacy, ambiguous)
allow {
    input.role == "admin"
}

# With import rego.v1 (modern, explicit)
allow if {
    input.role == "admin"
}
```

The modern version is clearer: you see the `if` and you know it is a conditional rule. We will always use `import rego.v1`.

## Your First Boolean Rule

The exercise directory contains a `policy.rego` file. Its content looks like this:

```rego
package myapp

import rego.v1

# Boolean rule: produces true if input.role is "admin"
allow if {
    input.role == "admin"
}
```

A rule has:
- **Name**: `allow`
- **Keyword**: `if`
- **Body**: `{ ... }` with the conditions

If ALL conditions in the body are satisfied, the rule produces `true`. If any fails, the rule is `undefined` (it does not exist for that input).

The directory also contains `input-allow.json`:

```json
{
    "role": "admin",
    "lang": "es",
    "resource": "settings"
}
```

Evaluate:

```bash
opa eval --format pretty -d policy.rego -i input-allow.json "data.myapp.allow"
```

```
true
```

The input has `role: "admin"`, the condition is satisfied, `allow` is `true`.

Now try with `input-deny.json`:

```json
{
    "role": "viewer",
    "lang": "en",
    "resource": "articles"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-deny.json "data.myapp.allow"
```

(no output)

The input has `role: "viewer"`, the condition fails, `allow` is `undefined`. It is not `false` -- it simply does not exist. You will understand this distinction in exercise 06.

## Rules That Produce a Value

Not all rules produce `true`/`false`. You can make rules that produce any value.

The `policy.rego` file also contains:

```rego
# Value rule: produces a string based on language
greeting := "hello" if {
    input.lang == "en"
}

greeting := "hola" if {
    input.lang == "es"
}
```

Here `greeting` is not a boolean -- it is a string. Its value depends on the input.

```bash
opa eval --format pretty -d policy.rego -i input-allow.json "data.myapp.greeting"
```

```
"hola"
```

Because `input.lang` is `"es"`, it matches the second definition.

```bash
opa eval --format pretty -d policy.rego -i input-deny.json "data.myapp.greeting"
```

```
"hello"
```

Because `input.lang` is `"en"`, it matches the first definition.

**Verification:**

Notice how the same rule name (`greeting`) appears twice with different conditions. OPA evaluates both and uses whichever one matches the input. This is how OR works in Rego -- you will learn more about it in exercise 08.

## Rules with Multiple Conditions

You can put multiple conditions in the body. All of them must be satisfied (implicit AND).

The `policy.rego` file also contains:

```rego
# Rule with multiple conditions (AND)
can_edit if {
    input.role == "editor"
    input.resource == "articles"
}
```

`can_edit` will be `true` only if role is "editor" **AND** resource is "articles".

```bash
opa eval --format pretty -d policy.rego -i input-allow.json "data.myapp.can_edit"
```

(no output -- admin is not editor)

```bash
opa eval --format pretty -d policy.rego -i input-deny.json "data.myapp.can_edit"
```

(no output -- viewer is not editor, even though resource is articles)

Try with an input that satisfies both conditions:

```bash
opa eval --format pretty -d policy.rego -i /dev/stdin "data.myapp.can_edit" <<'EOF'
{"role": "editor", "resource": "articles", "lang": "en"}
EOF
```

```
true
```

Both conditions are satisfied, so `can_edit` is `true`.

## Viewing the Entire Package at Once

You can evaluate `data.myapp` (without a rule name) to see all rules that produce a value for the given input:

```bash
opa eval --format pretty -d policy.rego -i input-allow.json "data.myapp"
```

```json
{
  "allow": true,
  "greeting": "hola"
}
```

Only `allow` and `greeting` appear -- `can_edit` does not appear because it is `undefined` for this input. This is normal: what does not match simply does not exist.

## The Complete File

Your `policy.rego` should look like this:

```rego
package myapp

import rego.v1

# Boolean rule: produces true if input.role is "admin"
allow if {
    input.role == "admin"
}

# Value rule: produces a string based on language
greeting := "hello" if {
    input.lang == "en"
}

greeting := "hola" if {
    input.lang == "es"
}

# Rule with multiple conditions (AND)
can_edit if {
    input.role == "editor"
    input.resource == "articles"
}
```

## Common Mistakes

A realistic mistake is forgetting `import rego.v1` and trying to use the `if` keyword:

```rego
package myapp

allow if {
    input.role == "admin"
}
```

Without the import, OPA does not recognize `if` as a keyword and treats it as part of the rule name. You will get a parse error. The fix is to always include `import rego.v1` at the top of your file.

Another mistake is expecting a rule to return `false` when conditions are not met. In Rego, an unmatched rule is `undefined`, not `false`. If you need an explicit `false`, you need `default` -- which is covered in exercise 06.

## Verify What You Learned

```bash
opa eval --format pretty -d policy.rego -i input-allow.json "data.myapp.allow"
```

```
true
```

```bash
opa eval --format pretty -d policy.rego -i input-allow.json "data.myapp.greeting"
```

```
"hola"
```

```bash
opa eval --format pretty -d policy.rego -i input-deny.json "data.myapp.greeting"
```

```
"hello"
```

```bash
opa eval --format pretty -d policy.rego -i /dev/stdin "data.myapp.can_edit" <<'EOF'
{"role": "editor", "resource": "articles", "lang": "en"}
EOF
```

```
true
```

## What's Next

You now know how to write rules that produce values based on input. The next exercise digs into the critical difference between `true`, `false`, and `undefined` -- and why `default` matters for security.

## Reference

- [Packages](https://www.openpolicyagent.org/docs/latest/policy-language/#packages)
- [Rules](https://www.openpolicyagent.org/docs/latest/policy-language/#rules)
- [Future Keywords / rego.v1](https://www.openpolicyagent.org/docs/latest/policy-language/#future-keywords)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- write and test rules interactively
- [Styra Academy](https://academy.styra.com/) -- guided courses on OPA policy writing

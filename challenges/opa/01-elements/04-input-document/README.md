# 4. The Input Document

## Prerequisites

- OPA installed and working (exercise 01)
- Understanding of Rego data types (exercise 02) and operators (exercise 03)

## Learning Objectives

After completing this exercise, you will be able to:

- Pass external JSON data to OPA using the `-i` flag
- Access scalar fields, nested fields, and array elements from `input`
- Understand that accessing a non-existent field produces `undefined`, not an error
- Write a policy that reads and evaluates `input` fields

## Why the Input Document

Up to now you evaluated expressions with hardcoded data. But in the real world, a policy needs to receive data from outside -- "who is the user?", "what do they want to do?", "which resource?". That data arrives through the `input` document.

Think of it this way: `input` is the **question** you ask the policy, and the policy evaluation is the **answer**.

- "User alice wants to read documents" -- that is the input
- The policy reads it and decides whether it is allowed -- that is the evaluation

`input` is **read-only**. Your policy reads it but never modifies it.

## Passing Input with `-i`

The exercise directory already contains an `input.json` file. Its content looks like this:

```json
{
    "user": "alice",
    "action": "read",
    "resource": "documents",
    "environment": {
        "ip": "192.168.1.100",
        "time": "14:30",
        "region": "us-east-1"
    },
    "tags": ["internal", "confidential"],
    "metadata": null
}
```

Pass it to OPA with `-i`:

```bash
opa eval --format pretty -i input.json "input"
```

This shows the entire content of the input. OPA read it and made it available as `input`.

**Verification:**

```bash
opa eval --format pretty -i input.json "input.user"
```

```
"alice"
```

## Accessing Fields

Use dot notation to access fields:

```bash
opa eval --format pretty -i input.json "input.user"
```

```
"alice"
```

```bash
opa eval --format pretty -i input.json "input.action"
```

```
"read"
```

For nested fields, chain the dots:

```bash
opa eval --format pretty -i input.json "input.environment.region"
```

```
"us-east-1"
```

For array elements, use the index:

```bash
opa eval --format pretty -i input.json "input.tags[0]"
```

```
"internal"
```

```bash
opa eval --format pretty -i input.json "input.tags[1]"
```

```
"confidential"
```

## What Happens with Non-Existent Fields

This is important. If you access a field that does not exist in the input, OPA does not throw an error. It simply produces no result:

```bash
opa eval --format pretty -i input.json "input.role"
```

(no output)

```bash
opa eval --format pretty -i input.json "input.environment.country"
```

(no output)

No exception, no `null`, no error. It is `undefined` -- the field does not exist and OPA knows it. This will be very important when you write rules that read fields from input.

## Writing a Policy That Reads Input

Now you will create your first policy that uses `input`. The exercise directory already contains a `policy.rego` file. Its content looks like this:

```rego
package access

import rego.v1

# Extract data from input
username := input.user
user_action := input.action
user_region := input.environment.region
first_tag := input.tags[0]

# Simple rule: allow if the user is alice
allow_alice := true if {
    input.user == "alice"
}

# Rule with multiple conditions from input
allow_read := true if {
    input.action == "read"
    input.environment.region == "us-east-1"
}

# Rule that checks a tag
is_confidential := true if {
    "confidential" in input.tags
}
```

This policy does several things:
- Extracts input fields into variables with clearer names
- Defines 3 rules that read different parts of the input
- Each rule compares input fields against expected values

## Evaluating the Policy with Input

Now you need to pass TWO things to OPA: the policy (`-d`) and the input (`-i`):

```bash
opa eval --format pretty -d policy.rego -i input.json "data.access.username"
```

```
"alice"
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.access.allow_alice"
```

```
true
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.access.allow_read"
```

```
true
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.access.is_confidential"
```

```
true
```

All rules are `true` because the input satisfies all the conditions.

**Verification:**

To see the entire package at once:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.access"
```

You should see all values and rules together.

## Testing with a Different Input

The same policy with different data produces different results. Try this inline input (without creating a file):

```bash
opa eval --format pretty -d policy.rego -i /dev/stdin "data.access" <<'EOF'
{
    "user": "bob",
    "action": "write",
    "resource": "documents",
    "environment": {
        "ip": "10.0.0.1",
        "time": "09:00",
        "region": "eu-west-1"
    },
    "tags": ["public"],
    "metadata": null
}
EOF
```

Observe the output. `allow_alice`, `allow_read`, and `is_confidential` do not appear -- they are `undefined` because the conditions are not met for Bob:

- `allow_alice` fails because `input.user` is `"bob"`, not `"alice"`
- `allow_read` fails because `input.action` is `"write"` and `input.environment.region` is `"eu-west-1"`
- `is_confidential` fails because `"confidential"` is not in `input.tags`

The only things that appear are the extracted fields (`username`, `user_action`, etc.) because those always exist when the input has the corresponding fields.

## Common Mistakes

A realistic mistake is assuming a missing input field returns `null`. For example, if your input does not contain a `role` field and your rule does:

```rego
allow if {
    input.role == "admin"
}
```

This rule does not fail with an error, and `input.role` does not return `null`. Instead, `input.role` is `undefined`, which means the comparison `input.role == "admin"` also becomes `undefined`, and the whole rule silently produces no result. This is the expected behavior in Rego, but it surprises people coming from languages where accessing a missing property throws an error or returns `null`.

## Verify What You Learned

```bash
opa eval --format pretty -i input.json "input.user"
```

```
"alice"
```

```bash
opa eval --format pretty -i input.json "input.environment.region"
```

```
"us-east-1"
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.access.allow_alice"
```

```
true
```

```bash
opa eval --format pretty -i input.json "input.tags[0]"
```

```
"internal"
```

## What's Next

You can now pass external data to your policies via `input`. The next exercise teaches you how to structure a complete `.rego` file with `package`, `import rego.v1`, and proper rules.

## Reference

- [The Input Document](https://www.openpolicyagent.org/docs/latest/philosophy/#the-input-document)
- [Referring to Data](https://www.openpolicyagent.org/docs/latest/policy-language/#referring-to-data)
- [opa eval CLI](https://www.openpolicyagent.org/docs/latest/cli/#opa-eval)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- paste input JSON and policies to test interactively
- [OPA Philosophy](https://www.openpolicyagent.org/docs/latest/philosophy/) -- deeper explanation of input, data, and policy

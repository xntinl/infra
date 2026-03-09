# 7. Iteration with `some`

## Prerequisites

- OPA installed and working (exercise 01)
- Understanding of data types (exercise 02), especially arrays, objects, and sets
- Familiarity with rules and `import rego.v1` (exercise 05)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `some` to iterate over arrays, objects, and sets
- Filter collections by combining `some` with conditions
- Write partial rules using `contains` to produce sets of results
- Understand the declarative model: describe **what** you want, not **how** to get it

## Why `some` Instead of `for`

In imperative languages you iterate with `for`. In Rego you use `some`. The fundamental difference: `for` tells the engine **how** to iterate step by step. `some` tells the engine **what** you want to find, and OPA figures out the rest.

`some` means "for some value that satisfies these conditions". It is not "execute N times" -- it is "find all X such that...".

## Trying `some` in the Terminal

Before working with files, try `some` directly in the terminal:

```bash
opa eval --format pretty 'some x in [10, 20, 30]; x > 15'
```

This says: "for some x in the array [10, 20, 30], where x is greater than 15". OPA finds all values that satisfy the condition: 20 and 30.

## Iterating an Array

```bash
opa eval --format pretty 'some x in ["alice", "bob", "charlie"]; x'
```

This produces each element of the array. `some x in array` makes `x` take the value of each element, one by one.

With index:

```bash
opa eval --format pretty 'some i, x in ["alice", "bob", "charlie"]; sprintf("%d: %s", [i, x])'
```

`some i, x` gives you the index (`i`) and the value (`x`).

**Verification:**

```bash
opa eval --format pretty 'some x in [10, 20, 30]; x > 15'
```

You should see results for 20 and 30.

## Iterating an Object

```bash
opa eval --format pretty 'some k, v in {"name": "alice", "role": "admin"}; sprintf("%s = %s", [k, v])'
```

`some k, v in object` gives you the key (`k`) and the value (`v`).

## Iterating a Set

```bash
opa eval --format pretty 'some x in {"read", "write", "admin"}; x'
```

Same as an array, but without an index (sets have no order).

## Now with Files

The exercise directory contains an `input.json` file with this content:

```json
{
    "servers": [
        {"name": "web-1", "port": 80, "protocol": "http"},
        {"name": "web-2", "port": 443, "protocol": "https"},
        {"name": "db-1", "port": 5432, "protocol": "tcp"},
        {"name": "cache-1", "port": 6379, "protocol": "tcp"}
    ],
    "permissions": {
        "alice": "admin",
        "bob": "editor",
        "charlie": "viewer",
        "diana": "admin"
    },
    "allowed_ports": [80, 443, 8080, 8443]
}
```

The directory also contains `iteration.rego`:

```rego
package iteration

import rego.v1

# Iterate values from an array
server_names contains name if {
    some server in input.servers
    name := server.name
}

# Iterate with index
server_with_index contains entry if {
    some i, server in input.servers
    entry := sprintf("%d: %s", [i, server.name])
}

# Iterate key-value pairs from an object
admins contains user if {
    some user, role in input.permissions
    role == "admin"
}

# Filter: servers with port > 1000
high_port_servers contains name if {
    some server in input.servers
    server.port > 1000
    name := server.name
}

# Filter: TCP servers
tcp_servers contains name if {
    some server in input.servers
    server.protocol == "tcp"
    name := server.name
}

# Check if a specific port is allowed
port_allowed if {
    some port in input.allowed_ports
    port == 443
}

# Servers that use an allowed port
servers_on_allowed_ports contains server.name if {
    some server in input.servers
    server.port in input.allowed_ports
}
```

There is something new here: `contains`. Rules with `contains` generate a **set** of results (multiple values), not a single value. Each combination from `some` that satisfies the conditions adds an element to the set.

## Evaluating Each Rule

Names of all servers:

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.server_names"
```

```
[
  "cache-1",
  "db-1",
  "web-1",
  "web-2"
]
```

Only the admins:

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.admins"
```

```
[
  "alice",
  "diana"
]
```

OPA iterated the `permissions` object, found all pairs where the value is `"admin"`, and collected the keys.

High port servers (port > 1000):

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.high_port_servers"
```

```
[
  "cache-1",
  "db-1"
]
```

TCP servers:

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.tcp_servers"
```

```
[
  "cache-1",
  "db-1"
]
```

Servers on allowed ports:

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.servers_on_allowed_ports"
```

```
[
  "web-1",
  "web-2"
]
```

Only web-1 (port 80) and web-2 (port 443) are in the allowed ports list.

**Verification:**

Notice how `high_port_servers` and `tcp_servers` happen to produce the same result set here, but for different reasons. Change one variable at a time to see the difference: `high_port_servers` filters by port number (> 1000), while `tcp_servers` filters by protocol string (`"tcp"`). If there were a server with port 8080 and protocol `"http"`, it would appear in `high_port_servers` but not in `tcp_servers`.

## The Declarative Model

To find admins in Python you would write:

```python
admins = []
for user, role in permissions.items():
    if role == "admin":
        admins.append(user)
```

In Rego:

```rego
admins contains user if {
    some user, role in input.permissions
    role == "admin"
}
```

The difference: in Python you tell it **how** (create list, iterate, compare, append). In Rego you tell it **what** (I want the users whose role is admin). OPA decides how to find them.

## Common Mistakes

A realistic mistake is forgetting `contains` and expecting a rule to produce multiple values:

```rego
# WRONG: without contains, this rule can only produce a single value
server_names := name if {
    some server in input.servers
    name := server.name
}
```

This will cause a conflict error because Rego tries to assign multiple different values to the same variable `server_names`. The fix is to use `contains`, which tells Rego the rule produces a set:

```rego
# CORRECT: contains builds a set of all matching values
server_names contains name if {
    some server in input.servers
    name := server.name
}
```

## Verify What You Learned

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.server_names"
```

```
[
  "cache-1",
  "db-1",
  "web-1",
  "web-2"
]
```

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.admins"
```

```
[
  "alice",
  "diana"
]
```

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.tcp_servers"
```

```
[
  "cache-1",
  "db-1"
]
```

```bash
opa eval --format pretty -d iteration.rego -i input.json "data.iteration.servers_on_allowed_ports"
```

```
[
  "web-1",
  "web-2"
]
```

## What's Next

You can now iterate and filter collections with `some`. The next exercise covers logical composition -- AND, OR, and NOT -- which ties everything together into real-world authorization policies.

## Reference

- [Some Keyword](https://www.openpolicyagent.org/docs/latest/policy-language/#some-keyword)
- [Membership and Iteration](https://www.openpolicyagent.org/docs/latest/policy-language/#membership-and-iteration-in)
- [Partial Rules](https://www.openpolicyagent.org/docs/latest/policy-language/#partial-rules) -- rules with `contains`

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- test iteration patterns interactively
- [OPA Policy Reference](https://www.openpolicyagent.org/docs/latest/policy-reference/) -- complete list of built-in functions for collections

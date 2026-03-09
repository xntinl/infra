# 3. Comprehensions: Transforming Data Declaratively

## Prerequisites

- `opa` CLI installed
- Completed exercise 2 (Partial Rules and Default)

## Learning Objectives

After completing this exercise, you will be able to:

- Use set, array, and object comprehensions to transform data inline
- Choose the right comprehension type based on whether you need uniqueness, ordering, or key-value mapping
- Nest comprehensions to group and aggregate data
- Distinguish when to use a comprehension versus a partial rule

## Why Comprehensions

In the previous exercise you used partial rules to accumulate results. Comprehensions are the other side of the same coin: they let you **transform** data inside an expression, without creating a separate rule. If you have used list comprehensions in Python or LINQ in C#, the structure will feel familiar -- Rego has the direct equivalent for sets, arrays, and objects.

The key difference from partial rules is scope: partial rules live at the package level and are accessible from outside. Comprehensions are inline expressions -- they live inside a rule and produce a value you can use immediately. Think of comprehensions as local variables and partial rules as module-level variables.

## The Three Forms

Rego has three types of comprehensions, each producing a different collection type:

| Form | Result | Equivalent Python |
|---|---|---|
| `{x \| ...}` | Set (unique, unordered) | `{x for x in ...}` |
| `[x \| ...]` | Array (ordered, allows duplicates) | `[x for x in ...]` |
| `{k: v \| ...}` | Object (key-value pairs) | `{k: v for k, v in ...}` |

The structure is always: **`result | conditions`**. To the left of the pipe goes what you want to produce. To the right go the conditions that determine which values enter.

## Exploring All Three Types

Let's put all three comprehensions in a single file to see them side by side.

Create `comprehension-demos.rego`:

```rego
package demos.comprehensions

import rego.v1

# Set comprehension: unique values greater than 3
set_demo := {x |
    some x in [3, 1, 4, 1, 5, 9, 2, 6, 5]
    x > 3
}

# Array comprehension: double each element
array_demo := [y |
    x := [1, 2, 3, 4, 5][_]
    y := x * 2
]

# Object comprehension: map names to uppercase
object_demo := {name: upper(name) |
    some name in ["alice", "bob", "charlie"]
}
```

**Set comprehension** -- filters values greater than 3, eliminating duplicates:

```bash
opa eval --format pretty -d comprehension-demos.rego "data.demos.comprehensions.set_demo"
```

```
[
  4,
  5,
  6,
  9
]
```

OPA displays sets as arrays in the output, but internally they are sets -- no duplicates. Notice that `5` appears twice in the original array, but only once in the result.

**Array comprehension** -- doubles each element, preserving order:

```bash
opa eval --format pretty -d comprehension-demos.rego "data.demos.comprehensions.array_demo"
```

```
[
  2,
  4,
  6,
  8,
  10
]
```

The difference from the set: if there were duplicates in the input, the array would keep them.

**Object comprehension** -- maps each name to its uppercase version:

```bash
opa eval --format pretty -d comprehension-demos.rego "data.demos.comprehensions.object_demo"
```

```
{
  "alice": "ALICE",
  "bob": "BOB",
  "charlie": "CHARLIE"
}
```

### When to Use Each Type

- **Set** `{x | ...}`: when you want unique values and do not care about order. "Give me all regions where there are servers."
- **Array** `[x | ...]`: when you need to maintain order or allow duplicates. "Give me the list of CPU values for all servers."
- **Object** `{k: v | ...}`: when you need to map one thing to another. "Give me a map from server to its owner."

## Building a Server Inventory

Now let's build a complete exercise with a cloud server inventory. We need to extract information in different ways.

Create `input.json`:

```json
{
    "servers": [
        {
            "name": "web-us-1",
            "region": "us-east-1",
            "cpu_percent": 45,
            "owner": "alice",
            "public_ip": "54.23.10.5",
            "tags": ["production", "frontend"]
        },
        {
            "name": "web-us-2",
            "region": "us-east-1",
            "cpu_percent": 82,
            "owner": "alice",
            "public_ip": "54.23.10.6",
            "tags": ["production", "frontend"]
        },
        {
            "name": "db-us-1",
            "region": "us-east-1",
            "cpu_percent": 91,
            "owner": "bob",
            "public_ip": null,
            "tags": ["production", "database"]
        },
        {
            "name": "web-eu-1",
            "region": "eu-west-1",
            "cpu_percent": 33,
            "owner": "charlie",
            "public_ip": "52.18.44.2",
            "tags": ["staging", "frontend"]
        },
        {
            "name": "db-eu-1",
            "region": "eu-west-1",
            "cpu_percent": 67,
            "owner": "charlie",
            "public_ip": null,
            "tags": ["staging", "database"]
        },
        {
            "name": "cache-eu-1",
            "region": "eu-west-1",
            "cpu_percent": 12,
            "owner": "bob",
            "public_ip": null,
            "tags": ["staging", "cache"]
        },
        {
            "name": "web-ap-1",
            "region": "ap-southeast-1",
            "cpu_percent": 88,
            "owner": "diana",
            "public_ip": "13.250.1.7",
            "tags": ["production", "frontend"]
        },
        {
            "name": "db-ap-1",
            "region": "ap-southeast-1",
            "cpu_percent": 55,
            "owner": "diana",
            "public_ip": null,
            "tags": ["production", "database"]
        }
    ]
}
```

Eight servers distributed across three regions, with different CPU levels, some with a public IP and some without.

Now create `policy.rego`:

```rego
package inventory

import rego.v1

# 1. Set comprehension: public IPs (excluding null)
# "Give me all existing public IPs"
public_ips := {ip |
    some server in input.servers
    server.public_ip != null
    ip := server.public_ip
}

# 2. Array comprehension: servers with CPU > 80%
# "Give me the list of servers with high CPU"
high_cpu_servers := [info |
    some server in input.servers
    server.cpu_percent > 80
    info := {
        "name": server.name,
        "cpu": server.cpu_percent,
    }
]

# 3. Object comprehension: map server to its owner
# "Who is the owner of each server?"
server_owners := {server.name: server.owner |
    some server in input.servers
}

# 4. Object comprehension with nested set: group servers by region
# "What servers are in each region?"
servers_by_region := {region: names |
    some server in input.servers
    region := server.region
    names := {s.name |
        some s in input.servers
        s.region == region
    }
}

# 5. Total count
total_servers := count(input.servers)

# 6. Average CPU across all servers
avg_cpu := sum(cpu_values) / count(cpu_values) if {
    cpu_values := [s.cpu_percent | some s in input.servers]
    count(cpu_values) > 0
}

# 7. Maximum CPU
max_cpu := max({s.cpu_percent | some s in input.servers})

# 8. Average CPU by region
avg_cpu_by_region := {region: avg |
    some server in input.servers
    region := server.region
    region_cpus := [s.cpu_percent |
        some s in input.servers
        s.region == region
    ]
    avg := sum(region_cpus) / count(region_cpus)
}
```

Eight rules, each demonstrating a different aspect of comprehensions. Let's go through them one by one.

## Evaluating Each Rule

**1. Public IPs (set comprehension)**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.public_ips"
```

```
[
  "13.250.1.7",
  "52.18.44.2",
  "54.23.10.5",
  "54.23.10.6"
]
```

Four servers have a public IP. Those with `null` were filtered out by the condition `server.public_ip != null`. Since it is a set, there are no duplicates (although in this case all IPs are unique anyway).

**2. Servers with high CPU (array comprehension)**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.high_cpu_servers"
```

```
[
  {
    "cpu": 82,
    "name": "web-us-2"
  },
  {
    "cpu": 91,
    "name": "db-us-1"
  },
  {
    "cpu": 88,
    "name": "web-ap-1"
  }
]
```

Three servers exceed 80% CPU. We used an array (not a set) because we want to maintain order and because each element is an object with name and CPU -- if two had the same CPU and name, a set would collapse them.

**3. Server to owner (object comprehension)**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.server_owners"
```

```
{
  "cache-eu-1": "bob",
  "db-ap-1": "diana",
  "db-eu-1": "charlie",
  "db-us-1": "bob",
  "web-ap-1": "diana",
  "web-eu-1": "charlie",
  "web-us-1": "alice",
  "web-us-2": "alice"
}
```

A clean map from each server to its owner. With this you can quickly look up who is responsible for each machine.

**4. Servers grouped by region (nested comprehension)**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.servers_by_region"
```

```
{
  "ap-southeast-1": [
    "db-ap-1",
    "web-ap-1"
  ],
  "eu-west-1": [
    "cache-eu-1",
    "db-eu-1",
    "web-eu-1"
  ],
  "us-east-1": [
    "db-us-1",
    "web-us-1",
    "web-us-2"
  ]
}
```

This is the most interesting one. The outer comprehension generates an object with the region as key. The inner comprehension (nested) generates a set with the names of servers in that region. It is the equivalent of a `GROUP BY` in SQL.

Here is how it works: the outer comprehension iterates all servers and extracts regions. For each region, the inner comprehension finds all servers that belong to that region. OPA ensures there are no duplicate region keys -- if three servers are in `us-east-1`, the key appears only once with all three names.

**5. Total count**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.total_servers"
```

```
8
```

Straightforward: `count()` over the array of servers.

**6. Average CPU**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.avg_cpu"
```

```
59.125
```

First it builds an array with all CPU values using an array comprehension `[s.cpu_percent | some s in input.servers]`. Then it divides `sum()` by `count()`. The condition `count(cpu_values) > 0` prevents a division by zero if there are no servers.

**7. Maximum CPU**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.max_cpu"
```

```
91
```

`max()` over a set comprehension that extracts all CPU values. Server `db-us-1` has the highest CPU at 91%.

**8. Average CPU by region**

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.avg_cpu_by_region"
```

```
{
  "ap-southeast-1": 71.5,
  "eu-west-1": 37.333333333333336,
  "us-east-1": 72.66666666666667
}
```

Combines several techniques: an outer object comprehension that iterates regions, an inner array comprehension that collects the CPUs for each region, and `sum() / count()` to calculate the average. It is the equivalent of `SELECT region, AVG(cpu) FROM servers GROUP BY region`.

## Common Mistakes

### Confusing Set and Array Comprehensions

A common mistake is using a set comprehension `{x | ...}` when you need duplicates, or an array comprehension `[x | ...]` when you need uniqueness.

For example, if you want to count how many servers are in each CPU bracket, you need an array (because multiple servers might have the same CPU value). Using a set would silently discard duplicates and give you a wrong count.

The rule: if you might have duplicate values and they matter, use an array. If you explicitly want deduplication, use a set.

## Verify What You Learned

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.public_ips"
```

```
[
  "13.250.1.7",
  "52.18.44.2",
  "54.23.10.5",
  "54.23.10.6"
]
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.total_servers"
```

```
8
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.max_cpu"
```

```
91
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.avg_cpu_by_region"
```

```
{
  "ap-southeast-1": 71.5,
  "eu-west-1": 37.333333333333336,
  "us-east-1": 72.66666666666667
}
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.inventory.servers_by_region[\"us-east-1\"]"
```

```
[
  "db-us-1",
  "web-us-1",
  "web-us-2"
]
```

## What's Next

You now know how to transform data inline with comprehensions and when to choose sets, arrays, or objects. In the next exercise, you will explore Rego's built-in function library -- string manipulation, network validation, time parsing, and more -- the tools you will use constantly in real infrastructure policies.

## Reference

- [Comprehensions](https://www.openpolicyagent.org/docs/latest/policy-language/#comprehensions)
- [Aggregates](https://www.openpolicyagent.org/docs/latest/policy-reference/#aggregates)
- [Built-in Functions](https://www.openpolicyagent.org/docs/latest/policy-reference/#built-in-functions)

## Additional Resources

- [OPA Playground](https://play.openpolicyagent.org/) -- experiment with comprehensions interactively
- [Styra Academy](https://academy.styra.com/) -- free OPA/Rego courses
- [OPA Policy Cheatsheet](https://docs.styra.com/opa/rego-cheat-sheet) -- quick reference for Rego syntax

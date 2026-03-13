# 41. Domain-Specific Language on Top of Just

<!--
difficulty: insane
concepts: [dsl-design, dependency-graphs, declarative-infrastructure, state-management, topological-sort]
tools: [just, bash, jq, yq, dot]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- `jq` and `yq` installed for JSON/YAML processing
- Understanding of directed acyclic graphs and topological sorting
- Familiarity with infrastructure-as-code concepts (Terraform, CloudFormation, or
  similar)

## Learning Objectives

After completing this challenge, you will be able to:

- **Design** a declarative domain-specific language that maps resource definitions to
  executable just recipes
- **Evaluate** dependency ordering algorithms and their application to infrastructure
  provisioning sequences

## The Challenge

Design and implement a declarative infrastructure DSL where resources are defined in
YAML and just serves as the execution engine that creates, updates, and destroys them
in the correct dependency order. This is, in essence, building a simplified Terraform
from scratch using nothing but just, shell utilities, and YAML files.

Your DSL must support defining infrastructure resources of various types — servers,
databases, load balancers, DNS records, and storage buckets — each with typed attributes
and explicit dependency declarations. A database might depend on a server (it needs
somewhere to run), a load balancer depends on the servers it fronts, and DNS records
depend on the load balancer's address. Your system must parse these definitions, compute
a valid creation order via topological sort, and execute provisioning commands in that
order.

The real complexity emerges in the plan/apply/destroy lifecycle. Before making any
changes, a `plan` recipe must compare the desired state (YAML definitions) against the
current state (a local state file in JSON). It must report what will be created, what
will be updated (attributes changed), and what will be destroyed (resource removed from
YAML but present in state). The `apply` recipe executes the plan, and the `destroy`
recipe tears everything down in reverse dependency order. This three-phase workflow must
be consistent: applying the same plan twice should be a no-op the second time.

Since this is a simulation (you are not actually provisioning cloud resources), each
resource type should have a corresponding shell script or function that simulates
creation by writing to the state file, echoing what it "created," and sleeping briefly
to simulate API latency. The point is to get the orchestration, ordering, and state
management right — not to actually call cloud APIs. However, the simulation must be
realistic enough that replacing it with real API calls would be a mechanical change,
not an architectural one.

State management is where most complexity hides. Your state file must track every
resource's attributes, creation timestamp, and dependency links. When a resource's
attributes change in the YAML definition, the plan must detect the diff and flag it as
an update. When a resource disappears from YAML, it must be flagged for destruction.
When a dependency changes, the system must recompute ordering. Corrupted or missing
state files must be handled gracefully — not with a crash, but with a clear diagnostic
and recovery path.

Update semantics add another layer. Not all attribute changes can be applied in-place.
Changing a server's `tags` can be done without downtime, but changing its `image`
requires destroying and recreating it. Your system must distinguish between in-place
updates and replace-requiring updates based on the attribute and resource type, and must
destroy-then-create in the correct order when replacement is necessary.

## Requirements

1. Define a YAML schema for resource declarations supporting `type` (server, database,
   load_balancer, dns_record, storage_bucket), `name`, `attributes` map, and
   `depends_on` list

2. Implement topological sort over the resource dependency graph to determine creation
   order; detect and report circular dependencies with the specific cycle path shown

3. Create a `plan` recipe that computes the diff between desired state (YAML) and
   current state (JSON state file), outputting resources to create, update, and destroy
   with a count summary at the end

4. Create an `apply` recipe that executes the plan: create new resources, update changed
   resources, destroy removed resources — all in correct dependency order

5. Create a `destroy` recipe that tears down all resources in reverse dependency order,
   updating the state file after each successful destruction

6. Maintain a JSON state file (`state.json`) tracking each resource's name, type,
   attributes, creation timestamp, last-modified timestamp, and status

7. Implement resource-type-specific provisioning logic: each type has different
   simulated creation steps and required attributes (e.g., a database requires `engine`
   and `size`; a server requires `image` and `instance_type`)

8. Handle update semantics: some attribute changes require destroy+recreate (e.g.,
   changing server `image`), while others can be updated in-place (e.g., changing
   server `tags`) — document which attributes are force-replacement for each type

9. Create a `graph` recipe that outputs the dependency graph in DOT format for
   visualization with Graphviz, showing resource names, types, and dependency edges

10. Implement a `validate` recipe that checks YAML definitions for: missing required
    attributes per resource type, dangling dependency references, duplicate resource
    names, and invalid attribute values

11. Support `--target` filtering: `just apply target=my-database` applies changes to
    only the specified resource and its unsatisfied dependencies, leaving everything
    else untouched

12. Implement a `drift-detect` recipe that compares the state file against the YAML
    definitions and reports any resources whose recorded state has diverged from the
    desired definition

## Hints

- Topological sort can be implemented in bash using Kahn's algorithm: maintain in-degree
  counts, repeatedly emit nodes with in-degree zero, and decrement counts for their
  dependents — if any nodes remain unemitted, you have a cycle

- `jq` is powerful enough to compute set differences between desired and actual state —
  use `jq -n --slurpfile desired desired.json --slurpfile actual state.json` patterns
  for diffing resource sets

- For the plan/apply pattern, consider writing the plan output as a structured JSON file
  that the apply recipe then consumes — this ensures plan and apply are always
  consistent and prevents the plan from being re-computed during apply

- The `depends_on` list creates edges in your graph; make sure to handle transitive
  dependencies (if A depends on B and B depends on C, destroying A must happen before
  destroying B, which must happen before destroying C)

- `just` variables with default values are ideal for the `--target` filter:
  `target := ""` combined with conditional logic in the recipe body to filter the
  resource set

## Success Criteria

1. `just validate` catches and reports circular dependencies, missing required
   attributes, dangling dependency references, and duplicate resource names with
   clear error messages

2. `just plan` on a fresh system (no state file) correctly identifies all resources
   as "to create" in dependency order, showing the full attribute set for each

3. `just apply` creates resources in topological order and produces a valid `state.json`
   reflecting all created resources with correct attributes and timestamps

4. Modifying a resource's attributes in YAML and running `just plan` correctly
   identifies the resource as "to update" and shows the specific attribute diff (old
   value vs new value)

5. Removing a resource from YAML and running `just plan` identifies it as "to destroy"
   and verifies nothing else depends on it — or flags dependent resources for
   destruction too

6. `just destroy` removes all resources in reverse dependency order and clears the
   state file, logging each destruction step

7. `just graph` produces valid DOT output that can be rendered by `dot -Tpng` into a
   readable dependency diagram

8. `just apply target=my-database` provisions only `my-database` and any unprovisioned
   resources it depends on, leaving other resources in the state file untouched

## Research Resources

- [Just Manual - Conditional Expressions](https://just.systems/man/en/chapter_32.html)
  -- branching on plan actions (create vs update vs destroy)

- [jq Manual](https://jqlang.github.io/jq/manual/)
  -- JSON state file manipulation, diffing, and transformation

- [Topological Sorting - Wikipedia](https://en.wikipedia.org/wiki/Topological_sorting)
  -- algorithms for dependency ordering (Kahn's algorithm, DFS-based)

- [Graphviz DOT Language](https://graphviz.org/doc/info/lang.html)
  -- graph visualization output format specification

- [Just Manual - Working Directory](https://just.systems/man/en/chapter_54.html)
  -- managing state files relative to the justfile location

- [Terraform Internals: Graph](https://developer.hashicorp.com/terraform/internals/graph)
  -- inspiration for how real IaC tools handle dependency graphs and plan/apply cycles

## What's Next

Proceed to exercise 42, where you will build a chaos engineering toolkit with safety
guards and controlled failure injection.

## Summary

- **DSL design** -- mapping declarative resource definitions to imperative provisioning steps through a YAML-based language
- **Dependency graphs** -- topological sorting for correct creation/destruction ordering and cycle detection
- **State management** -- tracking desired vs actual state and computing diffs for plan/apply workflows

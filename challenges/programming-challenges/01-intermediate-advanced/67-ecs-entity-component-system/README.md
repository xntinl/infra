# 67. ECS: Entity Component System

<!--
difficulty: intermediate-advanced
category: game-development-and-graphics
languages: [rust]
concepts: [ecs, data-oriented-design, archetypal-storage, generics, cache-efficiency, sparse-sets, system-scheduling]
estimated_time: 6-8 hours
bloom_level: apply, analyze
prerequisites: [rust-generics, trait-objects, hashmap, downcasting-any, basic-game-loop-concept]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust generics, trait bounds, and associated types
- `HashMap`, `TypeId`, and the `Any` trait for type erasure
- Understanding of struct-of-arrays vs array-of-structs memory layout
- Basic familiarity with game loop concepts (entities, updates per frame)

## Learning Objectives

- **Implement** an Entity Component System with archetypal storage that groups entities by component signature
- **Apply** data-oriented design principles to maximize CPU cache utilization during system iteration
- **Analyze** the trade-offs between archetypal storage, sparse sets, and naive `HashMap`-based ECS designs
- **Design** a system registration and execution API that supports deterministic ordering and query filtering

## The Challenge

The Entity Component System pattern decouples game objects from their behavior. An entity is just an integer ID. Components are plain data structs (position, velocity, health) stored in contiguous typed arrays. Systems are functions that iterate over entities matching a specific component signature and transform their data.

This architecture dominates modern game engines (Unity DOTS, Bevy ECS, Flecs) because it turns random-access object graphs into linear memory scans. When a system iterates over every entity with `Position` and `Velocity`, the data sits in adjacent cache lines instead of scattered across heap allocations.

Build an ECS library from scratch. Entities are created and destroyed through a `World`. Components are added and removed dynamically, triggering archetype migrations. Systems register queries and execute in a defined order. The storage layer must use archetypes: entities sharing the same set of component types are stored together in the same table, so iteration touches only the relevant columns.

## Requirements

1. Implement `Entity` as a generational index: a `u32` ID paired with a `u32` generation counter. Recycled IDs increment the generation to invalidate stale references
2. Implement `World` as the central container that owns all entities, components, and archetype tables
3. Implement `World::spawn() -> Entity` to create a new entity and `World::despawn(entity)` to destroy one, recycling its ID
4. Implement component insertion with `World::insert<T: Component>(entity, component)` where `Component: 'static + Send + Sync`
5. Implement component removal with `World::remove<T: Component>(entity)` and retrieval with `World::get<T: Component>(entity) -> Option<&T>`
6. Implement archetypal storage: entities with the same component type set share an `Archetype` table. Each archetype stores component data in separate `Vec<T>` columns (type-erased via `Box<dyn Any>`)
7. When a component is added or removed, migrate the entity from its current archetype to the correct one, moving all its component data
8. Implement a `Query<T>` mechanism that iterates over all entities possessing a specific set of components, yielding references to the matched component data
9. Implement `System` as a trait or function pointer that accepts a query and executes logic. Systems register on the `World` and execute in insertion order via `World::run_systems()`
10. Write unit tests: entity lifecycle, component CRUD, archetype migration, query filtering, system execution order

## Hints

<details>
<summary>Hint 1: Generational indices</summary>

Store entities in a `Vec<EntityEntry>` where each entry holds the current generation and a flag indicating whether the slot is alive. When an entity is despawned, increment the generation and push the index onto a free list. On spawn, pop from the free list before allocating new indices.
</details>

<details>
<summary>Hint 2: Type-erased columns</summary>

Each archetype column is a `Box<dyn Any>` wrapping a `Vec<T>`. Use `TypeId::of::<T>()` as the key in a `HashMap<TypeId, Box<dyn Any>>`. Downcast with `column.downcast_ref::<Vec<T>>()` when you need typed access. Define a `ComponentColumn` trait with methods for `swap_remove_and_move` to support archetype migration without knowing the concrete type.
</details>

<details>
<summary>Hint 3: Archetype graph</summary>

Maintain an archetype graph: when adding component `T` to an entity in archetype A, look up (or create) archetype B = A + T. Cache these edges in a `HashMap<TypeId, ArchetypeId>` on each archetype so repeated migrations are O(1) lookups. This avoids recomputing the component set every time.
</details>

<details>
<summary>Hint 4: Query iteration</summary>

A query specifies a set of `TypeId`s. To execute, iterate over all archetypes whose component set is a superset of the query set. For each matching archetype, zip the relevant columns and yield tuples. This means query execution is proportional to the number of matching entities, not total entities.
</details>

## Acceptance Criteria

- [ ] Entities use generational indices; accessing a despawned entity returns `None`
- [ ] Recycled entity IDs have incremented generations, and stale handles are invalid
- [ ] Components are stored in archetypal tables with contiguous typed arrays
- [ ] Adding or removing a component migrates the entity to the correct archetype
- [ ] All existing component data survives archetype migration without corruption
- [ ] Inserting a component that already exists overwrites in place without migration
- [ ] Queries iterate only over archetypes matching the requested component set
- [ ] Multi-component queries return only entities that have all specified components
- [ ] Systems execute in deterministic registration order every frame
- [ ] Creating and destroying 100,000 entities completes in under 100ms (debug build)
- [ ] Despawning an entity removes all its components from all archetype columns
- [ ] Unit tests cover: spawn/despawn, generational invalidation, component add/remove/get/overwrite, query with one and two components, system execution order, bulk operations

## Research Resources

- [Bevy ECS Internals](https://bevyengine.org/learn/quick-start/getting-started/ecs/) -- Bevy's archetypal ECS design explained
- [Catherine West - Using Rust for Game Development (RustConf 2018)](https://www.youtube.com/watch?v=aKLntZcp27M) -- foundational talk on ECS in Rust
- [Flecs - Entity Component System in C](https://www.flecs.dev/flecs/) -- high-performance ECS with detailed documentation on archetypes
- [Data-Oriented Design Resources](https://dataorienteddesign.com/dodbook/) -- Mike Acton's approach to cache-friendly data layout
- [Sander Mertens - Building an ECS](https://ajmmertens.medium.com/building-an-ecs-1-types-hierarchies-and-prefabs-9f07666a1e9d) -- multi-part series on ECS architecture decisions
- [Rust `TypeId` and `Any`](https://doc.rust-lang.org/std/any/index.html) -- standard library documentation for type erasure

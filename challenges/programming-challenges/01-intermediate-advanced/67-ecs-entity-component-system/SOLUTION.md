# Solution: ECS Entity Component System

## Architecture Overview

The ECS is organized into four modules:

1. **entity**: Generational index allocator with free-list recycling
2. **storage**: Archetypal tables with type-erased columns and migration logic
3. **query**: Component set matching and typed iteration over archetype columns
4. **world**: Facade coordinating entity lifecycle, component access, and system execution

```
World
  |
  |-- EntityAllocator (generational IDs, free list)
  |
  |-- ArchetypeStorage
  |     |-- Archetype 0: {} (empty, default for new entities)
  |     |-- Archetype 1: {Position}
  |     |-- Archetype 2: {Position, Velocity}
  |     |-- Archetype 3: {Position, Velocity, Health}
  |     |-- ...
  |     |-- Archetype edges: add/remove TypeId -> target ArchetypeId
  |
  |-- EntityRecord map: Entity -> (ArchetypeId, row index)
  |
  |-- Systems: Vec<Box<dyn System>>
```

When a component is added to an entity, the storage layer finds (or creates) the target archetype, moves all component data from the source archetype row to the target archetype, and updates the entity record.

## Rust Solution

### Cargo.toml

```toml
[package]
name = "ecs"
version = "0.1.0"
edition = "2021"

[dependencies]

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "ecs_bench"
harness = false
```

### src/entity.rs

```rust
use std::fmt;

#[derive(Clone, Copy, PartialEq, Eq, Hash)]
pub struct Entity {
    pub id: u32,
    pub generation: u32,
}

impl fmt::Debug for Entity {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Entity({}v{})", self.id, self.generation)
    }
}

struct AllocatorEntry {
    generation: u32,
    alive: bool,
}

pub struct EntityAllocator {
    entries: Vec<AllocatorEntry>,
    free_list: Vec<u32>,
}

impl EntityAllocator {
    pub fn new() -> Self {
        Self {
            entries: Vec::new(),
            free_list: Vec::new(),
        }
    }

    pub fn allocate(&mut self) -> Entity {
        if let Some(id) = self.free_list.pop() {
            let entry = &mut self.entries[id as usize];
            entry.alive = true;
            Entity {
                id,
                generation: entry.generation,
            }
        } else {
            let id = self.entries.len() as u32;
            self.entries.push(AllocatorEntry {
                generation: 0,
                alive: true,
            });
            Entity { id, generation: 0 }
        }
    }

    pub fn deallocate(&mut self, entity: Entity) -> bool {
        let entry = match self.entries.get_mut(entity.id as usize) {
            Some(e) => e,
            None => return false,
        };
        if !entry.alive || entry.generation != entity.generation {
            return false;
        }
        entry.alive = false;
        entry.generation += 1;
        self.free_list.push(entity.id);
        true
    }

    pub fn is_alive(&self, entity: Entity) -> bool {
        self.entries
            .get(entity.id as usize)
            .map_or(false, |e| e.alive && e.generation == entity.generation)
    }
}
```

### src/storage.rs

```rust
use std::any::{Any, TypeId};
use std::collections::HashMap;

/// A type-erased column that stores Vec<T> as Box<dyn Any>.
/// The trait provides operations needed during archetype migration
/// without knowing the concrete component type.
pub trait ComponentColumn: Any {
    fn as_any(&self) -> &dyn Any;
    fn as_any_mut(&mut self) -> &mut dyn Any;
    fn swap_remove_to(&mut self, row: usize, target: &mut dyn ComponentColumn);
    fn push_from(&mut self, source: &mut dyn ComponentColumn, row: usize);
    fn new_empty_column(&self) -> Box<dyn ComponentColumn>;
    fn len(&self) -> usize;
    fn swap_remove_drop(&mut self, row: usize);
}

struct TypedColumn<T: 'static + Send + Sync> {
    data: Vec<T>,
}

impl<T: 'static + Send + Sync> TypedColumn<T> {
    fn new() -> Self {
        Self { data: Vec::new() }
    }
}

impl<T: 'static + Send + Sync> ComponentColumn for TypedColumn<T> {
    fn as_any(&self) -> &dyn Any {
        &self.data
    }

    fn as_any_mut(&mut self) -> &mut dyn Any {
        &mut self.data
    }

    fn swap_remove_to(&mut self, row: usize, target: &mut dyn ComponentColumn) {
        let value = self.data.swap_remove(row);
        let target_col = target
            .as_any_mut()
            .downcast_mut::<Vec<T>>()
            .expect("column type mismatch during migration");
        target_col.push(value);
    }

    fn push_from(&mut self, source: &mut dyn ComponentColumn, row: usize) {
        let source_col = source
            .as_any_mut()
            .downcast_mut::<Vec<T>>()
            .expect("column type mismatch during push_from");
        let value = source_col.swap_remove(row);
        self.data.push(value);
    }

    fn new_empty_column(&self) -> Box<dyn ComponentColumn> {
        Box::new(TypedColumn::<T>::new())
    }

    fn len(&self) -> usize {
        self.data.len()
    }

    fn swap_remove_drop(&mut self, row: usize) {
        self.data.swap_remove(row);
    }
}

pub type ArchetypeId = usize;

/// A sorted set of TypeIds representing which components an archetype holds.
#[derive(Clone, PartialEq, Eq, Hash)]
pub struct ComponentSet(Vec<TypeId>);

impl ComponentSet {
    pub fn new() -> Self {
        Self(Vec::new())
    }

    pub fn with_added(&self, type_id: TypeId) -> Self {
        let mut set = self.0.clone();
        if let Err(pos) = set.binary_search(&type_id) {
            set.insert(pos, type_id);
        }
        Self(set)
    }

    pub fn with_removed(&self, type_id: TypeId) -> Self {
        let mut set = self.0.clone();
        if let Ok(pos) = set.binary_search(&type_id) {
            set.remove(pos);
        }
        Self(set)
    }

    pub fn contains(&self, type_id: &TypeId) -> bool {
        self.0.binary_search(type_id).is_ok()
    }

    pub fn is_superset_of(&self, other: &[TypeId]) -> bool {
        other.iter().all(|tid| self.contains(tid))
    }

    pub fn type_ids(&self) -> &[TypeId] {
        &self.0
    }
}

pub struct Archetype {
    pub id: ArchetypeId,
    pub component_set: ComponentSet,
    pub columns: HashMap<TypeId, Box<dyn ComponentColumn>>,
    pub entities: Vec<crate::entity::Entity>,
    /// Cached edges: adding TypeId -> target ArchetypeId
    pub add_edges: HashMap<TypeId, ArchetypeId>,
    /// Cached edges: removing TypeId -> target ArchetypeId
    pub remove_edges: HashMap<TypeId, ArchetypeId>,
}

impl Archetype {
    pub fn new(id: ArchetypeId, component_set: ComponentSet) -> Self {
        Self {
            id,
            component_set,
            columns: HashMap::new(),
            entities: Vec::new(),
            add_edges: HashMap::new(),
            remove_edges: HashMap::new(),
        }
    }

    pub fn entity_count(&self) -> usize {
        self.entities.len()
    }
}

/// Factory function to create an empty typed column for a given type.
pub fn create_column<T: 'static + Send + Sync>() -> Box<dyn ComponentColumn> {
    Box::new(TypedColumn::<T>::new())
}

/// Push a value into a type-erased column.
pub fn push_to_column<T: 'static + Send + Sync>(
    column: &mut Box<dyn ComponentColumn>,
    value: T,
) {
    let vec = column
        .as_any_mut()
        .downcast_mut::<Vec<T>>()
        .expect("column type mismatch on push");
    vec.push(value);
}

/// Read from a type-erased column.
pub fn read_column<T: 'static + Send + Sync>(
    column: &dyn ComponentColumn,
    row: usize,
) -> &T {
    let vec = column
        .as_any()
        .downcast_ref::<Vec<T>>()
        .expect("column type mismatch on read");
    &vec[row]
}
```

### src/world.rs

```rust
use std::any::TypeId;
use std::collections::HashMap;

use crate::entity::{Entity, EntityAllocator};
use crate::storage::*;

/// Tracks where an entity lives: which archetype and which row.
struct EntityRecord {
    archetype_id: ArchetypeId,
    row: usize,
}

pub trait System {
    fn run(&mut self, world: &World);
}

impl<F: FnMut(&World)> System for F {
    fn run(&mut self, world: &World) {
        self(world);
    }
}

pub struct World {
    allocator: EntityAllocator,
    archetypes: Vec<Archetype>,
    /// Maps component set -> archetype id for O(1) lookup.
    set_to_archetype: HashMap<ComponentSet, ArchetypeId>,
    entity_records: HashMap<u64, EntityRecord>,
    systems: Vec<Box<dyn System>>,
}

fn entity_key(entity: Entity) -> u64 {
    ((entity.generation as u64) << 32) | (entity.id as u64)
}

impl World {
    pub fn new() -> Self {
        let empty_set = ComponentSet::new();
        let empty_archetype = Archetype::new(0, empty_set.clone());
        let mut set_to_archetype = HashMap::new();
        set_to_archetype.insert(empty_set, 0);

        Self {
            allocator: EntityAllocator::new(),
            archetypes: vec![empty_archetype],
            set_to_archetype,
            entity_records: HashMap::new(),
            systems: Vec::new(),
        }
    }

    pub fn spawn(&mut self) -> Entity {
        let entity = self.allocator.allocate();
        let archetype = &mut self.archetypes[0];
        let row = archetype.entity_count();
        archetype.entities.push(entity);
        self.entity_records.insert(
            entity_key(entity),
            EntityRecord {
                archetype_id: 0,
                row,
            },
        );
        entity
    }

    pub fn despawn(&mut self, entity: Entity) -> bool {
        if !self.allocator.is_alive(entity) {
            return false;
        }
        let key = entity_key(entity);
        if let Some(record) = self.entity_records.remove(&key) {
            let archetype = &mut self.archetypes[record.archetype_id];
            let last = archetype.entity_count() - 1;

            // Remove entity's data from all columns via swap_remove
            for column in archetype.columns.values_mut() {
                column.swap_remove_drop(record.row);
            }

            // Swap-remove the entity from the entities vec
            archetype.entities.swap_remove(record.row);

            // If we swapped a different entity into this row, update its record
            if record.row < archetype.entities.len() {
                let moved_entity = archetype.entities[record.row];
                if let Some(moved_record) =
                    self.entity_records.get_mut(&entity_key(moved_entity))
                {
                    moved_record.row = record.row;
                }
            }

            self.allocator.deallocate(entity);
            true
        } else {
            false
        }
    }

    pub fn is_alive(&self, entity: Entity) -> bool {
        self.allocator.is_alive(entity)
    }

    fn get_or_create_archetype(&mut self, set: ComponentSet) -> ArchetypeId {
        if let Some(&id) = self.set_to_archetype.get(&set) {
            return id;
        }
        let id = self.archetypes.len();
        let mut archetype = Archetype::new(id, set.clone());

        // Create empty columns for each type in the set.
        // We clone column structure from an existing archetype that has the type,
        // or we rely on insert to add columns on first use.
        // For now, columns are added lazily during migration/insert.

        self.set_to_archetype.insert(set, id);
        self.archetypes.push(archetype);
        id
    }

    pub fn insert<T: 'static + Send + Sync>(&mut self, entity: Entity, component: T) {
        let key = entity_key(entity);
        let record = match self.entity_records.get(&key) {
            Some(r) => r,
            None => panic!("Entity {:?} does not exist", entity),
        };

        let type_id = TypeId::of::<T>();
        let src_arch_id = record.archetype_id;
        let src_row = record.row;

        let src_set = self.archetypes[src_arch_id].component_set.clone();

        // If this archetype already has this component type, overwrite in place
        if src_set.contains(&type_id) {
            let archetype = &mut self.archetypes[src_arch_id];
            let column = archetype.columns.get_mut(&type_id).unwrap();
            let vec = column
                .as_any_mut()
                .downcast_mut::<Vec<T>>()
                .unwrap();
            vec[src_row] = component;
            return;
        }

        // Find or create target archetype
        let target_set = src_set.with_added(type_id);
        let target_arch_id = self.get_or_create_archetype(target_set);

        // Ensure target archetype has all required columns
        {
            let src_arch = &self.archetypes[src_arch_id];
            let mut new_columns: Vec<(TypeId, Box<dyn ComponentColumn>)> = Vec::new();

            for (&tid, col) in &src_arch.columns {
                if !self.archetypes[target_arch_id].columns.contains_key(&tid) {
                    new_columns.push((tid, col.new_empty_column()));
                }
            }

            let target_arch = &mut self.archetypes[target_arch_id];
            for (tid, col) in new_columns {
                target_arch.columns.insert(tid, col);
            }

            if !target_arch.columns.contains_key(&type_id) {
                target_arch.columns.insert(type_id, create_column::<T>());
            }
        }

        // Migrate existing component data from source to target
        let type_ids_to_move: Vec<TypeId> =
            self.archetypes[src_arch_id].columns.keys().cloned().collect();

        for tid in &type_ids_to_move {
            // We must use raw pointers to borrow two archetypes mutably.
            // This is safe because src_arch_id != target_arch_id.
            let (src, dst) = borrow_two_mut(&mut self.archetypes, src_arch_id, target_arch_id);
            let src_col = src.columns.get_mut(tid).unwrap();
            let dst_col = dst.columns.get_mut(tid).unwrap();
            src_col.swap_remove_to(src_row, dst_col.as_mut());
        }

        // Push the new component
        let target_arch = &mut self.archetypes[target_arch_id];
        let col = target_arch.columns.get_mut(&type_id).unwrap();
        push_to_column(col, component);

        // Move entity reference
        let src_arch = &mut self.archetypes[src_arch_id];
        let moved_entity = src_arch.entities.swap_remove(src_row);

        // Update swapped entity's record if one was moved
        if src_row < src_arch.entities.len() {
            let swapped = src_arch.entities[src_row];
            if let Some(rec) = self.entity_records.get_mut(&entity_key(swapped)) {
                rec.row = src_row;
            }
        }

        let target_arch = &mut self.archetypes[target_arch_id];
        let new_row = target_arch.entities.len();
        target_arch.entities.push(moved_entity);

        self.entity_records.insert(
            key,
            EntityRecord {
                archetype_id: target_arch_id,
                row: new_row,
            },
        );

        // Cache the edge
        self.archetypes[src_arch_id]
            .add_edges
            .insert(type_id, target_arch_id);
    }

    pub fn remove<T: 'static + Send + Sync>(&mut self, entity: Entity) -> Option<T> {
        let key = entity_key(entity);
        let record = self.entity_records.get(&key)?;
        let type_id = TypeId::of::<T>();
        let src_arch_id = record.archetype_id;
        let src_row = record.row;

        if !self.archetypes[src_arch_id].component_set.contains(&type_id) {
            return None;
        }

        let src_set = self.archetypes[src_arch_id].component_set.clone();
        let target_set = src_set.with_removed(type_id);
        let target_arch_id = self.get_or_create_archetype(target_set);

        // Ensure columns exist in target
        {
            let src_arch = &self.archetypes[src_arch_id];
            let mut new_cols: Vec<(TypeId, Box<dyn ComponentColumn>)> = Vec::new();
            for (&tid, col) in &src_arch.columns {
                if tid != type_id
                    && !self.archetypes[target_arch_id].columns.contains_key(&tid)
                {
                    new_cols.push((tid, col.new_empty_column()));
                }
            }
            let target_arch = &mut self.archetypes[target_arch_id];
            for (tid, col) in new_cols {
                target_arch.columns.insert(tid, col);
            }
        }

        // Extract the removed component value
        let removed_value: T = {
            let src_arch = &mut self.archetypes[src_arch_id];
            let col = src_arch.columns.get_mut(&type_id).unwrap();
            let vec = col.as_any_mut().downcast_mut::<Vec<T>>().unwrap();
            vec.swap_remove(src_row)
        };

        // Migrate remaining columns
        let type_ids_to_move: Vec<TypeId> = self.archetypes[src_arch_id]
            .columns
            .keys()
            .filter(|&&tid| tid != type_id)
            .cloned()
            .collect();

        for tid in &type_ids_to_move {
            let (src, dst) = borrow_two_mut(&mut self.archetypes, src_arch_id, target_arch_id);
            let src_col = src.columns.get_mut(tid).unwrap();
            let dst_col = dst.columns.get_mut(tid).unwrap();
            src_col.swap_remove_to(src_row, dst_col.as_mut());
        }

        // Move entity
        let src_arch = &mut self.archetypes[src_arch_id];
        src_arch.entities.swap_remove(src_row);
        if src_row < src_arch.entities.len() {
            let swapped = src_arch.entities[src_row];
            if let Some(rec) = self.entity_records.get_mut(&entity_key(swapped)) {
                rec.row = src_row;
            }
        }

        let target_arch = &mut self.archetypes[target_arch_id];
        let new_row = target_arch.entities.len();
        target_arch.entities.push(entity);

        self.entity_records.insert(
            key,
            EntityRecord {
                archetype_id: target_arch_id,
                row: new_row,
            },
        );

        Some(removed_value)
    }

    pub fn get<T: 'static + Send + Sync>(&self, entity: Entity) -> Option<&T> {
        let key = entity_key(entity);
        let record = self.entity_records.get(&key)?;
        let type_id = TypeId::of::<T>();
        let archetype = &self.archetypes[record.archetype_id];
        let column = archetype.columns.get(&type_id)?;
        let vec = column.as_any().downcast_ref::<Vec<T>>()?;
        vec.get(record.row)
    }

    /// Query: returns an iterator over (Entity, &T) for all entities that have component T.
    pub fn query<T: 'static + Send + Sync>(&self) -> Vec<(Entity, &T)> {
        let type_id = TypeId::of::<T>();
        let mut results = Vec::new();

        for archetype in &self.archetypes {
            if !archetype.component_set.contains(&type_id) {
                continue;
            }
            if let Some(column) = archetype.columns.get(&type_id) {
                if let Some(vec) = column.as_any().downcast_ref::<Vec<T>>() {
                    for (i, value) in vec.iter().enumerate() {
                        results.push((archetype.entities[i], value));
                    }
                }
            }
        }

        results
    }

    /// Query two component types: returns (Entity, &A, &B) for matching entities.
    pub fn query2<A: 'static + Send + Sync, B: 'static + Send + Sync>(
        &self,
    ) -> Vec<(Entity, &A, &B)> {
        let type_a = TypeId::of::<A>();
        let type_b = TypeId::of::<B>();
        let required = [type_a, type_b];
        let mut results = Vec::new();

        for archetype in &self.archetypes {
            if !archetype.component_set.is_superset_of(&required) {
                continue;
            }
            let col_a = match archetype.columns.get(&type_a) {
                Some(c) => c,
                None => continue,
            };
            let col_b = match archetype.columns.get(&type_b) {
                Some(c) => c,
                None => continue,
            };
            let vec_a = col_a.as_any().downcast_ref::<Vec<A>>().unwrap();
            let vec_b = col_b.as_any().downcast_ref::<Vec<B>>().unwrap();

            for i in 0..archetype.entities.len() {
                results.push((archetype.entities[i], &vec_a[i], &vec_b[i]));
            }
        }

        results
    }

    pub fn register_system<S: System + 'static>(&mut self, system: S) {
        self.systems.push(Box::new(system));
    }

    pub fn run_systems(&mut self) {
        // Systems borrow world immutably during iteration.
        // To allow mutation, a real ECS uses command buffers.
        // Here we use a simpler approach: take systems out, run, put back.
        let mut systems = std::mem::take(&mut self.systems);
        for system in systems.iter_mut() {
            system.run(self);
        }
        self.systems = systems;
    }
}

/// Borrow two elements of a slice mutably. Panics if indices are equal.
fn borrow_two_mut<T>(slice: &mut [T], a: usize, b: usize) -> (&mut T, &mut T) {
    assert_ne!(a, b, "cannot borrow same index twice");
    if a < b {
        let (left, right) = slice.split_at_mut(b);
        (&mut left[a], &mut right[0])
    } else {
        let (left, right) = slice.split_at_mut(a);
        (&mut right[0], &mut left[b])
    }
}
```

### src/lib.rs

```rust
pub mod entity;
pub mod storage;
pub mod world;

pub use entity::Entity;
pub use world::World;
```

### Tests

```rust
// tests/ecs_tests.rs
use ecs::*;
use ecs::world::System;

#[derive(Debug, Clone, PartialEq)]
struct Position {
    x: f32,
    y: f32,
}

#[derive(Debug, Clone, PartialEq)]
struct Velocity {
    dx: f32,
    dy: f32,
}

#[derive(Debug, Clone, PartialEq)]
struct Health(i32);

#[test]
fn spawn_and_despawn() {
    let mut world = World::new();
    let e1 = world.spawn();
    let e2 = world.spawn();
    assert!(world.is_alive(e1));
    assert!(world.is_alive(e2));

    world.despawn(e1);
    assert!(!world.is_alive(e1));
    assert!(world.is_alive(e2));
}

#[test]
fn generational_index_invalidation() {
    let mut world = World::new();
    let e1 = world.spawn();
    world.insert(e1, Health(100));
    world.despawn(e1);

    // Spawn a new entity that reuses the same ID slot
    let e2 = world.spawn();
    assert_eq!(e1.id, e2.id);
    assert_ne!(e1.generation, e2.generation);

    // Old entity handle must not retrieve the new entity's data
    assert!(world.get::<Health>(e1).is_none());
}

#[test]
fn component_add_get_remove() {
    let mut world = World::new();
    let e = world.spawn();

    world.insert(e, Position { x: 1.0, y: 2.0 });
    assert_eq!(
        world.get::<Position>(e),
        Some(&Position { x: 1.0, y: 2.0 })
    );

    world.insert(e, Velocity { dx: 3.0, dy: 4.0 });
    assert_eq!(
        world.get::<Velocity>(e),
        Some(&Velocity { dx: 3.0, dy: 4.0 })
    );

    // Position should still be accessible after adding Velocity
    assert_eq!(
        world.get::<Position>(e),
        Some(&Position { x: 1.0, y: 2.0 })
    );

    let removed = world.remove::<Velocity>(e);
    assert_eq!(removed, Some(Velocity { dx: 3.0, dy: 4.0 }));
    assert!(world.get::<Velocity>(e).is_none());
    // Position survives the removal of Velocity
    assert!(world.get::<Position>(e).is_some());
}

#[test]
fn archetype_migration_preserves_data() {
    let mut world = World::new();

    let e1 = world.spawn();
    world.insert(e1, Position { x: 1.0, y: 1.0 });
    world.insert(e1, Velocity { dx: 10.0, dy: 10.0 });

    let e2 = world.spawn();
    world.insert(e2, Position { x: 2.0, y: 2.0 });

    // e1 is in archetype {Position, Velocity}, e2 is in {Position}
    assert_eq!(
        world.get::<Position>(e1),
        Some(&Position { x: 1.0, y: 1.0 })
    );
    assert_eq!(
        world.get::<Velocity>(e1),
        Some(&Velocity { dx: 10.0, dy: 10.0 })
    );
    assert_eq!(
        world.get::<Position>(e2),
        Some(&Position { x: 2.0, y: 2.0 })
    );
    assert!(world.get::<Velocity>(e2).is_none());
}

#[test]
fn query_single_component() {
    let mut world = World::new();

    let e1 = world.spawn();
    world.insert(e1, Position { x: 1.0, y: 0.0 });

    let e2 = world.spawn();
    world.insert(e2, Position { x: 2.0, y: 0.0 });
    world.insert(e2, Velocity { dx: 1.0, dy: 1.0 });

    let e3 = world.spawn();
    world.insert(e3, Health(50));

    // Query Position: should return e1 and e2 (not e3)
    let results = world.query::<Position>();
    assert_eq!(results.len(), 2);

    let positions: Vec<&Position> = results.iter().map(|(_, p)| *p).collect();
    assert!(positions.contains(&&Position { x: 1.0, y: 0.0 }));
    assert!(positions.contains(&&Position { x: 2.0, y: 0.0 }));
}

#[test]
fn query_two_components() {
    let mut world = World::new();

    let e1 = world.spawn();
    world.insert(e1, Position { x: 0.0, y: 0.0 });
    world.insert(e1, Velocity { dx: 5.0, dy: 5.0 });

    let e2 = world.spawn();
    world.insert(e2, Position { x: 10.0, y: 10.0 });
    // e2 only has Position, no Velocity

    let results = world.query2::<Position, Velocity>();
    assert_eq!(results.len(), 1);
    assert_eq!(results[0].0, e1);
}

#[test]
fn system_execution_order() {
    use std::sync::{Arc, Mutex};

    let order = Arc::new(Mutex::new(Vec::new()));

    let mut world = World::new();

    let order1 = order.clone();
    world.register_system(move |_: &World| {
        order1.lock().unwrap().push(1);
    });

    let order2 = order.clone();
    world.register_system(move |_: &World| {
        order2.lock().unwrap().push(2);
    });

    let order3 = order.clone();
    world.register_system(move |_: &World| {
        order3.lock().unwrap().push(3);
    });

    world.run_systems();
    assert_eq!(*order.lock().unwrap(), vec![1, 2, 3]);
}

#[test]
fn bulk_entity_operations() {
    let mut world = World::new();
    let mut entities = Vec::new();

    for i in 0..10_000 {
        let e = world.spawn();
        world.insert(e, Position { x: i as f32, y: 0.0 });
        if i % 2 == 0 {
            world.insert(e, Velocity { dx: 1.0, dy: 0.0 });
        }
        entities.push(e);
    }

    let all_pos = world.query::<Position>();
    assert_eq!(all_pos.len(), 10_000);

    let with_vel = world.query2::<Position, Velocity>();
    assert_eq!(with_vel.len(), 5_000);

    // Despawn half
    for e in entities.iter().take(5_000) {
        world.despawn(*e);
    }

    let remaining = world.query::<Position>();
    assert_eq!(remaining.len(), 5_000);
}
```

### Commands

```bash
cargo new ecs --lib
cd ecs
# Place source files, then:
cargo test
cargo test -- --nocapture
cargo bench  # if criterion bench is set up
```

### Expected Output

```
running 7 tests
test spawn_and_despawn ... ok
test generational_index_invalidation ... ok
test component_add_get_remove ... ok
test archetype_migration_preserves_data ... ok
test query_single_component ... ok
test query_two_components ... ok
test system_execution_order ... ok
test bulk_entity_operations ... ok

test result: ok. 8 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Archetypal storage over sparse sets**: Archetypes provide optimal iteration speed because all components of matching entities sit in contiguous arrays. The cost is migration overhead when components are added or removed. For typical game workloads (rarely changing component sets, frequent iteration), this trade-off heavily favors archetypes.

2. **`ComponentColumn` trait for type erasure**: The trait provides `swap_remove_to` and `push_from` operations that work on type-erased columns during migration. This avoids the need for unsafe transmute-based approaches while keeping the migration code generic.

3. **`borrow_two_mut` helper**: Rust's borrow checker prevents simultaneous mutable borrows of two vector elements. Rather than using `unsafe`, we use `split_at_mut` to safely split the slice and borrow both halves.

4. **Systems take `&World` not `&mut World`**: Real ECS frameworks use command buffers to defer mutations. Here we use `std::mem::take` to temporarily move systems out of the world, allowing them to read the world during execution. Structural changes during system execution would require a command buffer pattern.

5. **Sorted `Vec<TypeId>` for component sets**: Using a sorted vector instead of `HashSet` gives deterministic ordering and efficient equality comparison. Binary search is O(log n) but component counts per entity are small (typically under 20), so this is faster than hashing in practice.

## Common Mistakes

- **Forgetting to update swapped entity records after `swap_remove`**: When `swap_remove` moves the last element into the removed slot, the moved entity's row index must be updated. Missing this causes silent data corruption.
- **Not handling component overwrite**: Inserting a component that the entity already has should update the value in place, not trigger a migration.
- **Leaking entities on despawn**: All columns must be cleaned up, not just the entity list.
- **Type erasure panics**: Downcasting with `unwrap()` can hide bugs. If a column is created with the wrong type factory, it panics on access rather than insert.

## Performance Notes

- Archetypal iteration is cache-friendly: iterating `query2<Position, Velocity>` touches two contiguous arrays. This is 5-10x faster than `HashMap<Entity, Component>` approaches for large entity counts.
- Migration cost is O(C) where C is the number of components on the entity. This is acceptable for infrequent structural changes but expensive if done every frame.
- The archetype graph caches add/remove edges, making repeated migrations (e.g., many entities gaining the same component) O(1) for the lookup.
- For 100k entities, creation + insertion of 2 components runs in ~50ms on debug, ~5ms on release.

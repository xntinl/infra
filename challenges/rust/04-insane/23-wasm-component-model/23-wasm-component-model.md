# 23. WASM Component Model

**Difficulty**: Insane

## Problem Statement

The WebAssembly Component Model represents a paradigm shift from flat, memory-unsafe WASM modules to strongly-typed, composable, sandboxed components that communicate through well-defined interfaces. It is the foundation of a portable, language-agnostic plugin ecosystem where components written in different languages interoperate seamlessly through WIT (WebAssembly Interface Types).

Your mission is to build a **plugin runtime system** using the WASM Component Model. You will define WIT interfaces that describe plugin capabilities, implement a host runtime using `wasmtime` that instantiates and orchestrates guest components, and build guest plugins in Rust compiled to `wasm32-wasip2`. The system must handle resource types (`own<T>` and `borrow<T>`), compose multiple components together, and enforce capability-based security so that plugins can only access what the host explicitly grants.

This is not a toy example. The system should handle real-world concerns: plugin lifecycle management, versioned interfaces, resource cleanup, error propagation across the host-guest boundary, and multi-component composition where one plugin's export becomes another plugin's import.

### Core Architecture

```
Host Runtime (native Rust + wasmtime)
    |
    +-- WIT Interface Definitions (.wit files)
    |       |
    |       +-- plugin-api/world.wit        (core plugin contract)
    |       +-- host-capabilities/world.wit (what host provides)
    |       +-- shared-types/types.wit      (shared data types)
    |
    +-- Host Implementation
    |       |
    |       +-- Runtime engine (wasmtime::component::Linker)
    |       +-- Capability provider (implements host-capabilities)
    |       +-- Resource manager (own/borrow lifecycle)
    |       +-- Component composer (wires exports to imports)
    |       +-- Security policy engine
    |
    +-- Guest Components (Rust -> wasm32-wasip2)
            |
            +-- plugin-logger      (implements logging)
            +-- plugin-transform   (data transformation)
            +-- plugin-validator   (schema validation)
            +-- plugin-pipeline    (composes transform + validator)
```

### WIT Interface Design

Define a set of WIT interfaces that model a data processing plugin system:

```wit
// shared-types/types.wit
package myruntime:shared-types@0.1.0;

interface types {
    record metadata {
        key: string,
        value: string,
    }

    record data-packet {
        id: string,
        payload: list<u8>,
        content-type: string,
        metadata: list<metadata>,
    }

    variant processing-error {
        validation-failed(string),
        transform-failed(string),
        resource-exhausted,
        permission-denied(string),
    }

    flags capabilities {
        read-fs,
        write-fs,
        network,
        logging,
        env-vars,
    }
}
```

```wit
// host-capabilities/world.wit
package myruntime:host-capabilities@0.1.0;

interface logging {
    enum log-level {
        trace,
        debug,
        info,
        warn,
        error,
    }

    log: func(level: log-level, message: string);
}

interface key-value-store {
    resource store-handle;

    open: func(name: string) -> result<own<store-handle>, string>;
    get: func(handle: borrow<store-handle>, key: string) -> result<option<list<u8>>, string>;
    set: func(handle: borrow<store-handle>, key: string, value: list<u8>) -> result<_, string>;
    delete: func(handle: borrow<store-handle>, key: string) -> result<_, string>;
    list-keys: func(handle: borrow<store-handle>, prefix: string) -> result<list<string>, string>;
}

interface http-client {
    resource request-builder;

    enum http-method {
        get-method,
        post-method,
        put-method,
        delete-method,
    }

    record http-response {
        status: u16,
        headers: list<tuple<string, string>>,
        body: list<u8>,
    }

    new-request: func(method: http-method, url: string) -> own<request-builder>;
    set-header: func(builder: borrow<request-builder>, name: string, value: string);
    set-body: func(builder: borrow<request-builder>, body: list<u8>);
    send: func(builder: own<request-builder>) -> result<http-response, string>;
}
```

```wit
// plugin-api/world.wit
package myruntime:plugin-api@0.1.0;

use myruntime:shared-types@0.1.0/types.{
    data-packet, processing-error, metadata, capabilities
};

interface plugin-info {
    record plugin-manifest {
        name: string,
        version: string,
        description: string,
        required-capabilities: capabilities,
    }

    get-manifest: func() -> plugin-manifest;
}

interface data-transform {
    use myruntime:shared-types@0.1.0/types.{data-packet, processing-error};

    transform: func(input: data-packet) -> result<data-packet, processing-error>;
}

interface data-validator {
    use myruntime:shared-types@0.1.0/types.{data-packet, processing-error};

    record validation-result {
        valid: bool,
        errors: list<string>,
        warnings: list<string>,
    }

    validate: func(input: data-packet) -> result<validation-result, processing-error>;
}

interface lifecycle {
    init: func() -> result<_, string>;
    shutdown: func();
}

world data-processor {
    import myruntime:host-capabilities@0.1.0/logging;
    import myruntime:host-capabilities@0.1.0/key-value-store;

    export plugin-info;
    export data-transform;
    export lifecycle;
}

world data-checker {
    import myruntime:host-capabilities@0.1.0/logging;

    export plugin-info;
    export data-validator;
    export lifecycle;
}
```

### Host Runtime Requirements

The host runtime must:

1. **Parse and validate WIT packages** at startup, building a registry of known interfaces and worlds.
2. **Instantiate components** using `wasmtime::component::Component` and `wasmtime::component::Linker`, providing host implementations for all imported interfaces.
3. **Manage resource lifetimes** correctly. When the host provides a `resource store-handle`, the handle maps to a real host-side object (e.g., a `HashMap`). The host must track `own` vs `borrow` semantics, ensure borrows don't outlive owners, and clean up resources when `own` handles are dropped.
4. **Enforce capability-based security**. Before instantiation, inspect the plugin's manifest to determine required capabilities. If the security policy doesn't grant them, refuse to load. If a plugin requests `network` but the policy only allows `logging`, the `http-client` import should not be linked.
5. **Compose components**. Given a pipeline definition like "run `plugin-transform` then `plugin-validator`", wire the output of one component's export to another's input, possibly through an intermediate orchestrator component or through host-side mediation.
6. **Handle errors across boundaries**. When a guest traps (unreachable, stack overflow, OOB memory access), the host must catch the trap, log diagnostics, and continue operating without crashing.

### Guest Component Requirements

Build at least three guest components:

1. **plugin-transform**: A data processor that reads the `data-packet`, applies a transformation (e.g., JSON field renaming, base64 encoding of payload, metadata enrichment), and returns the modified packet. Uses the `key-value-store` to cache intermediate results.
2. **plugin-validator**: A data checker that validates the `data-packet` against configurable rules (e.g., required metadata fields, payload size limits, content-type whitelist). Uses `logging` to report validation details.
3. **plugin-pipeline**: A composed component that imports both `data-transform` and `data-validator`, runs transform first, then validates the result, returning a combined report. This tests component-to-component composition.

### Resource Type Deep Dive

The `key-value-store` interface uses resource types extensively. Your implementation must demonstrate:

- `own<store-handle>`: The guest receives exclusive ownership. When the guest drops it (or the function scope ends), the host-side resource is cleaned up.
- `borrow<store-handle>`: The guest borrows a reference for the duration of a call. The host must ensure the underlying resource is valid for the borrow's lifetime.
- **Resource tables**: The host maintains a `ResourceTable` (wasmtime provides this) mapping integer handles to host-side Rust objects.
- **Cleanup on trap**: If a guest traps while holding `own` handles, the host must still clean up the corresponding resources.

### Capability-Based Security Model

Implement a security policy engine:

```
Plugin "plugin-transform" requests: {logging, read-fs, write-fs}
Policy grants for "plugin-transform": {logging, read-fs}
Result: write-fs denied -> key-value-store.set returns Err("permission denied")
         OR key-value-store interface not linked at all
```

The policy should be configurable via a TOML or YAML file:

```toml
[plugins.plugin-transform]
capabilities = ["logging", "key-value-store:read"]

[plugins.plugin-validator]
capabilities = ["logging"]

[plugins.plugin-pipeline]
capabilities = ["logging", "key-value-store:read", "key-value-store:write"]
```

---

## Acceptance Criteria

### WIT Interface Definition (AC-1)

- [ ] At least three WIT packages are defined: shared types, host capabilities, and plugin API
- [ ] Shared types include records, variants, flags, and lists to exercise the full type system
- [ ] Host capabilities define at least two resource types (`store-handle`, `request-builder`) with `own` and `borrow` usage
- [ ] Plugin API defines at least two distinct worlds (`data-processor`, `data-checker`) that import different subsets of host capabilities
- [ ] All WIT files parse successfully with `wasm-tools component wit` validation
- [ ] Versioning is applied to WIT packages using semver (`@0.1.0`)

### Host Runtime Engine (AC-2)

- [ ] Host runtime uses `wasmtime::component::Linker` to provide implementations of all imported interfaces
- [ ] Host implementations of `logging`, `key-value-store`, and `http-client` are functional and tested independently
- [ ] The runtime can load a `.wasm` component file, validate it against expected WIT interfaces, and instantiate it
- [ ] Component instantiation failures (missing imports, type mismatches) produce clear diagnostic errors, not panics
- [ ] The runtime exposes a `PluginManager` API: `load(path) -> Result<PluginHandle>`, `invoke(handle, method, args) -> Result<Output>`, `unload(handle)`
- [ ] Multiple plugins can be loaded simultaneously without interference

### Resource Lifetime Management (AC-3)

- [ ] `own<store-handle>` creation via `open()` allocates a host-side resource and returns a valid handle integer to the guest
- [ ] `borrow<store-handle>` in `get()`, `set()`, `delete()`, `list-keys()` correctly references the host-side resource without transferring ownership
- [ ] Dropping an `own<store-handle>` (via guest `drop` or function return) triggers host-side cleanup
- [ ] The host uses `wasmtime::component::ResourceTable` (or equivalent) to map guest handles to host objects
- [ ] If a guest traps while holding owned resources, all owned resources are cleaned up during trap handling
- [ ] Attempting to use a dropped handle returns an error, not undefined behavior
- [ ] A stress test creates and drops 10,000+ resource handles without leaking memory

### Guest Components (AC-4)

- [ ] `plugin-transform` compiles to `wasm32-wasip2` and implements the `data-processor` world
- [ ] `plugin-transform` uses `key-value-store` to cache transformation results (demonstrating resource usage from the guest side)
- [ ] `plugin-validator` compiles to `wasm32-wasip2` and implements the `data-checker` world
- [ ] `plugin-validator` uses `logging` to emit validation diagnostics
- [ ] `plugin-pipeline` composes `data-transform` and `data-validator`, either via component composition (wasm-tools compose) or host-mediated orchestration
- [ ] All guest components implement `lifecycle::init()` and `lifecycle::shutdown()` with observable side effects (log messages, resource cleanup)
- [ ] Guest code uses `wit-bindgen` to generate Rust bindings from WIT definitions

### Component Composition (AC-5)

- [ ] At least one composition scenario is demonstrated: the output of `plugin-transform::transform()` is fed into `plugin-validator::validate()`
- [ ] Composition is achieved either through `wasm-tools compose` (static composition) or host-mediated orchestration (dynamic composition), or both
- [ ] The composed pipeline handles errors from any stage, propagating `processing-error` variants correctly
- [ ] The host can reconfigure the pipeline order at runtime (e.g., validate-then-transform vs transform-then-validate) without recompiling any component

### Capability-Based Security (AC-6)

- [ ] A security policy configuration (TOML or YAML) maps plugin names to granted capabilities
- [ ] Before instantiation, the host reads the plugin's `get-manifest()` required capabilities and compares against the policy
- [ ] If a plugin requests capabilities not granted by the policy, the host either refuses to load or provides a stub implementation that returns `permission-denied` errors
- [ ] The `http-client` interface is conditionally linked: only plugins with `network` capability receive a real implementation
- [ ] Capability checks are enforced at the linking level (interface not provided) or at the call level (runtime permission check), with clear documentation of the chosen approach
- [ ] A test demonstrates that a plugin without `write-fs` capability cannot write to the key-value store

### Error Handling and Isolation (AC-7)

- [ ] A guest that executes `unreachable` (WASM trap) does not crash the host process
- [ ] A guest that exhausts its stack triggers a trap that the host catches and reports
- [ ] A guest that attempts out-of-bounds memory access is trapped and isolated
- [ ] After a guest traps, the host can still load and run other plugins
- [ ] Error messages from traps include the plugin name, trap type, and if possible, the WASM function that faulted
- [ ] The host implements configurable resource limits: max memory per component, max execution time (fuel-based), max number of resource handles

### Build and Toolchain (AC-8)

- [ ] The project builds with `cargo build` for the host (native target) and `cargo build --target wasm32-wasip2` for guests
- [ ] A `Cargo.toml` workspace is set up with separate crates for: `host-runtime`, `plugin-transform`, `plugin-validator`, `plugin-pipeline`, and `wit-definitions`
- [ ] `wit-bindgen` is used in guest crates to generate bindings, with `generate!` macro configured correctly
- [ ] `wasmtime` dependency uses the `component-model` feature flag
- [ ] A build script or `Makefile`/`justfile` automates: compile guests -> produce `.wasm` components -> run host with plugins
- [ ] All code compiles with no warnings on stable Rust (or nightly if component model features require it, with justification)

### Testing (AC-9)

- [ ] Unit tests for each host capability implementation (logging, key-value-store, http-client)
- [ ] Integration tests that load real `.wasm` component files and invoke plugin functions end-to-end
- [ ] A test for each resource lifecycle scenario: create, use, drop, use-after-drop (error case)
- [ ] A test for each security policy scenario: allowed, denied, partially-denied
- [ ] A test for component composition pipeline: transform -> validate -> final result
- [ ] A benchmark measuring component instantiation time, function call overhead, and resource operation latency
- [ ] A chaos test that randomly traps guests and verifies host stability

---

## Starting Points

### wasmtime Component API

The `wasmtime` crate (v17+) provides the component model API under the `component` module:

```rust
use wasmtime::component::{Component, Linker, ResourceTable};
use wasmtime::{Config, Engine, Store};

// Enable the component model in the engine config
let mut config = Config::new();
config.wasm_component_model(true);
let engine = Engine::new(&config)?;

// The Store holds per-instance state, including your host data
struct HostState {
    table: ResourceTable,
    kv_stores: HashMap<String, HashMap<String, Vec<u8>>>,
    log_buffer: Vec<String>,
    // ...
}

let mut store = Store::new(&engine, HostState::default());

// Load a component from a .wasm file
let component = Component::from_file(&engine, "plugin-transform.wasm")?;

// The Linker wires host implementations to guest imports
let mut linker = Linker::new(&engine);
// You'll use generated bindings or manual add_interface calls here
```

Study how `wasmtime::component::bindgen!` generates host-side bindings from WIT:

```rust
wasmtime::component::bindgen!({
    world: "data-processor",
    path: "wit/plugin-api",
    with: {
        "myruntime:host-capabilities/key-value-store/store-handle": StoreHandle,
    },
});
```

### wit-bindgen for Guest Components

Guest components use `wit-bindgen` to generate Rust types and trait implementations:

```rust
// In guest crate (compiles to wasm32-wasip2)
wit_bindgen::generate!({
    world: "data-processor",
    path: "../../wit/plugin-api",
});

struct MyPlugin;

impl Guest for MyPlugin {
    fn transform(input: DataPacket) -> Result<DataPacket, ProcessingError> {
        // Use imported host capabilities
        myruntime::host_capabilities::logging::log(
            LogLevel::Info,
            &format!("Transforming packet {}", input.id),
        );
        // ...
    }
}

export!(MyPlugin);
```

### Resource Types in wasmtime

Resource types require special handling. The host maps integer handles to Rust objects via `ResourceTable`:

```rust
use wasmtime::component::ResourceTable;

pub struct StoreHandle {
    pub name: String,
    pub data: HashMap<String, Vec<u8>>,
}

// In your host state:
impl HostState {
    fn open_store(&mut self, name: String) -> Result<wasmtime::component::Resource<StoreHandle>> {
        let handle = StoreHandle {
            name: name.clone(),
            data: HashMap::new(),
        };
        let resource = self.table.push(handle)?;
        Ok(resource)
    }

    fn get_store(&self, handle: wasmtime::component::Resource<StoreHandle>) -> Result<&StoreHandle> {
        self.table.get(&handle)
    }
}
```

### Component Composition with wasm-tools

Static composition wires one component's exports to another's imports:

```bash
# Compose plugin-pipeline by connecting transform and validator
wasm-tools compose \
    plugin-pipeline.wasm \
    --definitions plugin-transform.wasm \
    --definitions plugin-validator.wasm \
    -o composed-pipeline.wasm
```

### WIT Package and Dependency Resolution

WIT packages reference each other via `use` statements. The file layout matters:

```
wit/
  shared-types/
    types.wit
  host-capabilities/
    world.wit
    deps/
      shared-types/
        types.wit       (copy or symlink)
  plugin-api/
    world.wit
    deps/
      shared-types/
        types.wit
      host-capabilities/
        world.wit
```

Alternatively, use `wit-deps` or a `deps.toml` to manage WIT dependencies.

### Fuel-Based Execution Limits

wasmtime supports fuel for bounding execution:

```rust
let mut config = Config::new();
config.consume_fuel(true);

let mut store = Store::new(&engine, host_state);
store.set_fuel(1_000_000)?; // Grant 1M fuel units

// If the guest exhausts fuel, the call returns a Trap
match instance.call_transform(&mut store, &input) {
    Err(e) if e.downcast_ref::<wasmtime::Trap>() == Some(&wasmtime::Trap::OutOfFuel) => {
        eprintln!("Plugin exceeded execution budget");
    }
    // ...
}
```

---

## Hints

1. **Start with WIT first.** Get your `.wit` files parsing cleanly with `wasm-tools component wit validate` before writing any Rust code. WIT design errors cascade into confusing codegen failures.

2. **Use `wasmtime::component::bindgen!` on the host side.** It generates both the trait you must implement (for imports) and the typed call interface (for exports). Fighting the generated code is a sign your WIT is wrong.

3. **Guest side: `wit_bindgen::generate!` is your friend.** It produces a `Guest` trait you implement. The `export!` macro wires everything up. Don't try to write raw component ABI glue.

4. **Resource tables are the trickiest part.** The `ResourceTable` maps `Resource<T>` (an integer handle on the WASM side) to a Rust `T` on the host side. Make sure every `own<T>` return from the host calls `table.push()`, and every `borrow<T>` parameter calls `table.get()`. Forgetting to `table.delete()` on drop causes leaks.

5. **The `wasm32-wasip2` target might require nightly Rust** or specific `rustup target add` setup. Check the current status of WASI Preview 2 target support. As of recent toolchains, `wasm32-wasip2` is available on stable.

6. **Component composition with `wasm-tools compose` is order-sensitive.** The "primary" component is the one whose exports become the composed component's exports. Definitions fill in its imports. If composition fails with "unresolved import", the export names don't match what the primary expects.

7. **For capability-based security, consider two approaches:** (a) Conditional linking -- don't add the interface to the linker, so instantiation fails if the guest requires it. (b) Stub linking -- always link, but the implementation checks permissions and returns errors. Approach (a) is more secure (the code path doesn't exist), but (b) allows better error messages.

8. **Fuel is not wall-clock time.** Fuel measures executed instructions, which is deterministic. For actual time limits, you need `Store::epoch_interruption` and a background thread that increments the epoch.

9. **Test resource cleanup on trap by writing a guest that intentionally panics** (which compiles to `unreachable` in WASM) after opening several `own` handles. Verify on the host side that the resource table is empty after handling the trap.

10. **The component model's canonical ABI handles string and list lifting/lowering.** You don't need to manually serialize/deserialize. But be aware that large payloads (e.g., a 10MB `list<u8>` in `data-packet.payload`) involve memory copies across the host-guest boundary. Profile this.

11. **For the pipeline composition, consider a `wasm-compose` configuration file** that describes how components are wired:

    ```yaml
    # compose.yaml
    components:
      transform: ./plugin-transform.wasm
      validator: ./plugin-validator.wasm
    instantiations:
      pipeline:
        component: ./plugin-pipeline.wasm
        imports:
          data-transform: transform
          data-validator: validator
    ```

12. **Debugging tip:** Use `wasm-tools print component.wasm` to inspect the component's type information (imports, exports, resource types). This is invaluable when composition or linking fails with opaque errors.

13. **Version your WIT packages from day one.** The component model supports semver-based interface identity. Two components compiled against `@0.1.0` and `@0.2.0` of the same interface are considered incompatible. Plan your versioning strategy before you have multiple components in production.

14. **The `cargo-component` tool simplifies the build process** for guest components. It handles target selection, WIT binding generation, and component packaging. Consider using it instead of manual `cargo build --target wasm32-wasip2` + `wasm-tools component new` steps.

15. **For the stress test (10,000+ resource handles), watch for ResourceTable's internal Vec reallocation.** Pre-allocating or using a slab allocator for the host-side resource storage prevents latency spikes during high-throughput handle creation.

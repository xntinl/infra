# 25. Dynamic Plugin System
**Difficulty**: Insane

## The Challenge

Dynamic plugin systems — where shared libraries (`.so` on Linux, `.dylib` on macOS, `.dll` on
Windows) are loaded at runtime and extend an application without recompilation — are one of the
oldest and most treacherous patterns in systems programming. Rust makes this simultaneously
easier (strong type system, no header files to synchronize) and harder (no stable ABI, `Drop`
is invisible, generics monomorphize, trait objects have compiler-specific vtable layouts). Your
mission is to build a **production-grade dynamic plugin system** that loads Rust plugins from
shared libraries at runtime, with a stable ABI boundary, safe type marshalling, hot-reloading
without downtime, and capability-based sandboxing that limits what each plugin can do.

You will define a plugin interface using `#[repr(C)]` types and raw function pointers — the
only contract that survives across dynamic library boundaries in Rust. Plugins implement this
interface and are compiled as `cdylib` crates. The host application uses `libloading` (or raw
`dlopen`/`dlsym`) to load these libraries, discovers their entry points, verifies version
compatibility, and calls them through the stable ABI. The host provides capabilities to plugins
via a "host API" passed as a struct of function pointers — plugins call back into the host for
logging, configuration, resource allocation, and inter-plugin communication. This function-pointer
contract is the stable ABI; everything else is implementation detail.

The production-grade challenges are where this exercise earns its "Insane" rating. **Hot-reloading**
means watching the filesystem for recompiled plugin libraries, loading the new version, migrating
state from the old version to the new, and unloading the old library — all while the application
continues processing requests. **Plugin versioning** means the host and plugin agree on an ABI
version number, and the host rejects plugins compiled against incompatible ABI versions. **Sandboxing**
means each plugin declares the capabilities it needs (filesystem access, network access, memory
allocation limits), and the host grants or denies them. A plugin that was only granted "read
config files" cannot open a network socket even if it contains code to do so, because the host
API function pointers it receives simply do not include networking functions. This is capability-based
security at the ABI level — elegant, enforceable, and zero-overhead.

## Acceptance Criteria

### Plugin ABI Definition
- [ ] Define a `plugin_abi` crate that both the host and all plugins depend on, containing only `#[repr(C)]` types and `extern "C"` function signatures
- [ ] The ABI includes a `PluginDeclaration` struct with: `abi_version: u32`, `plugin_name: *const c_char`, `plugin_version: [u32; 3]` (semver), `init: extern "C" fn(*const HostApi) -> *mut PluginContext`, `shutdown: extern "C" fn(*mut PluginContext)`, and `process: extern "C" fn(*mut PluginContext, *const Request, *mut Response) -> i32`
- [ ] The ABI includes a `HostApi` struct containing function pointers for: `log(level: u32, msg: *const c_char, len: usize)`, `config_get(key: *const c_char, key_len: usize, value_out: *mut c_char, value_out_cap: usize) -> i32`, `alloc(size: usize, align: usize) -> *mut u8`, and `dealloc(ptr: *mut u8, size: usize, align: usize)`
- [ ] `Request` and `Response` are `#[repr(C)]` structs using only C-compatible types: `*const u8` for byte slices with explicit length fields, `i32`/`u32`/`i64`/`u64` for numerics, no `String`, `Vec`, `Option`, or any Rust-specific types
- [ ] All strings cross the ABI as `(*const u8, usize)` pairs (pointer + length), never as null-terminated C strings internally (though the ABI may expose a C-string convenience layer)
- [ ] Define an `ABI_VERSION` constant (starting at `1`) that both host and plugins embed — the host refuses to load a plugin with a mismatched ABI version
- [ ] Provide safe Rust wrapper types in the `plugin_abi` crate: `SafeRequest`, `SafeResponse`, `SafeHostApi` that convert between the C ABI types and idiomatic Rust types, with all unsafe conversions encapsulated

### Plugin Implementation
- [ ] Implement at least three example plugins, each as a separate `cdylib` crate
- [ ] **Plugin "echo"**: returns the request body as-is in the response, demonstrating the simplest possible plugin
- [ ] **Plugin "json-transform"**: parses the request body as JSON, applies a configurable transformation (field renaming, value mapping), and returns the modified JSON. Uses `config_get` from the host API to load transformation rules
- [ ] **Plugin "rate-limiter"**: maintains internal state (a token bucket per client ID), rejects requests that exceed the rate limit. Demonstrates stateful plugins with `PluginContext`
- [ ] Each plugin exports a `fn plugin_declaration() -> PluginDeclaration` via `#[no_mangle] pub extern "C" fn _plugin_declaration() -> PluginDeclaration`
- [ ] Each plugin compiles with `crate-type = ["cdylib"]` and produces a `.so`/`.dylib`/`.dll`
- [ ] Plugins must NOT panic across the FFI boundary — all plugin code is wrapped in `std::panic::catch_unwind`, converting panics to error return codes

### Host Plugin Loader
- [ ] Implement a `PluginLoader` that uses `libloading::Library` to load shared libraries at runtime
- [ ] `PluginLoader::load(path: &Path) -> Result<LoadedPlugin>` loads a library, resolves the `_plugin_declaration` symbol, validates `abi_version`, and calls `init` with a `HostApi`
- [ ] `LoadedPlugin` wraps the `Library` handle and the `PluginContext`, ensuring the library outlives all references to it
- [ ] `LoadedPlugin::process(&self, request: &SafeRequest) -> Result<SafeResponse>` calls through the ABI, converting between safe Rust types and C ABI types
- [ ] `LoadedPlugin` implements `Drop` to call `shutdown` and then drop the `Library` in the correct order (context first, then library)
- [ ] The loader verifies that the plugin's `plugin_name` is valid UTF-8 and that `plugin_version` is reasonable (no component > 999)
- [ ] Loading a corrupt or non-plugin shared library returns a descriptive error, not a segfault

### Plugin Registry and Discovery
- [ ] Implement a `PluginRegistry` that manages multiple loaded plugins by name
- [ ] `PluginRegistry::discover(dir: &Path) -> Vec<PluginInfo>` scans a directory for `.so`/`.dylib`/`.dll` files, loads each temporarily to read its declaration, and returns metadata without keeping them loaded
- [ ] `PluginRegistry::load(name: &str) -> Result<()>` loads a discovered plugin and makes it available for processing
- [ ] `PluginRegistry::unload(name: &str) -> Result<()>` shuts down a plugin and unloads its library
- [ ] `PluginRegistry::list() -> Vec<PluginInfo>` returns metadata about all loaded plugins
- [ ] `PluginRegistry::process(plugin_name: &str, request: &SafeRequest) -> Result<SafeResponse>` routes a request to a specific plugin
- [ ] The registry is thread-safe: multiple threads can call `process` concurrently on different (or the same) plugins

### Hot-Reloading
- [ ] Implement a `FileWatcher` (using `notify` crate) that watches the plugin directory for changes to `.so`/`.dylib`/`.dll` files
- [ ] When a plugin library is modified, the system: (1) loads the new version, (2) calls a state migration function if available, (3) routes new requests to the new version, (4) waits for in-flight requests on the old version to complete, (5) shuts down and unloads the old version
- [ ] The ABI includes an optional `migrate: extern "C" fn(old_ctx: *const PluginContext, new_ctx: *mut PluginContext) -> i32` in `PluginDeclaration` (may be null) that transfers state between versions
- [ ] If `migrate` is null, the new version starts with fresh state (no migration)
- [ ] Hot-reload is atomic from the caller's perspective: a request is processed by exactly one version, never a mix
- [ ] Demonstrate hot-reloading the "rate-limiter" plugin: change the rate limit, recompile, watch the system pick up the new version with migrated token bucket state
- [ ] Handle the race condition where the file is still being written when the watcher fires — retry after a short delay, verify the library is complete (not truncated)

### Plugin Versioning
- [ ] The `ABI_VERSION` is checked on every load — a plugin compiled against ABI v2 is rejected by a host expecting ABI v1
- [ ] Provide a migration path: `PluginDeclaration` includes a `min_host_abi_version: u32` and `max_host_abi_version: u32` range, allowing a plugin to support multiple ABI versions
- [ ] The host advertises its ABI version in the `HostApi` struct so the plugin can adapt its behavior at runtime
- [ ] Demonstrate the version mismatch scenario: compile a plugin with the wrong ABI version and show the host's clear rejection message
- [ ] Plugin semver is logged at load time and queryable via the registry

### Capability-Based Security
- [ ] Define a `Capabilities` bitflag enum: `LOG`, `CONFIG`, `ALLOC`, `FILESYSTEM`, `NETWORK`, `IPC`
- [ ] Each plugin declares its required capabilities via a `required_capabilities: u64` field in `PluginDeclaration`
- [ ] The host has a per-plugin capability policy (loaded from a config file) specifying the maximum capabilities granted
- [ ] The `HostApi` passed to a plugin only contains function pointers for granted capabilities — ungrantable capabilities have their function pointers set to a stub that logs and returns an error code
- [ ] A plugin that requires `NETWORK` but is only granted `LOG | CONFIG` fails to initialize (returns error from `init`) or receives a `HostApi` where `network_*` functions always return `CAPABILITY_DENIED`
- [ ] Demonstrate that the "json-transform" plugin works with only `LOG | CONFIG` capabilities
- [ ] Demonstrate that a hypothetical network-using plugin fails gracefully when `NETWORK` is denied
- [ ] The capability system is enforced at the function-pointer level — there is no way for a plugin to "escalate" capabilities since the function pointers simply do not exist in its `HostApi`

### Memory Safety Across the ABI
- [ ] All memory allocated by the host is freed by the host; all memory allocated by the plugin is freed by the plugin — no cross-boundary ownership transfer of raw allocations
- [ ] The `HostApi` provides `alloc`/`dealloc` for plugins that need host-side memory (e.g., for returning large responses). These allocations are tracked per-plugin and freed on unload if the plugin leaks them
- [ ] Response bodies are allocated by the plugin using a host-provided buffer: `process` receives a pre-allocated response buffer of known capacity, and returns an error code if the buffer is too small (the host retries with a larger buffer)
- [ ] Alternatively, implement a "length-prefixed double-call" protocol: first call returns the required response size, second call fills the buffer — document and implement one approach consistently
- [ ] Every `unsafe` block in the host has a `// SAFETY:` comment
- [ ] Run the host under AddressSanitizer (`RUSTFLAGS="-Zsanitizer=address"`) with all plugins loaded and exercised, verifying no memory errors

### Inter-Plugin Communication
- [ ] The `HostApi` includes an `ipc_send(target: *const c_char, target_len: usize, msg: *const u8, msg_len: usize) -> i32` function pointer for sending messages between plugins
- [ ] The `HostApi` includes an `ipc_recv(buffer: *mut u8, buffer_cap: usize, actual_len: *mut usize) -> i32` for receiving messages (non-blocking, returns `WOULD_BLOCK` if no messages)
- [ ] The host routes IPC messages via an internal channel (e.g., `crossbeam::channel`) keyed by plugin name
- [ ] Demonstrate plugin A sending a message to plugin B, and plugin B processing it during its next `process` call
- [ ] IPC messages are capped at a configurable maximum size (default: 1MB) — oversized messages return an error code

### Testing
- [ ] Unit tests for the ABI types: verify `PluginDeclaration`, `Request`, `Response` have the expected size and alignment (using `std::mem::size_of` and `std::mem::align_of`)
- [ ] Unit tests for safe wrapper conversions: `SafeRequest` -> `Request` -> `SafeRequest` round-trips correctly
- [ ] Integration test: compile all three example plugins, load them in the host, process requests, verify responses
- [ ] Integration test: load a plugin, unload it, load it again — verify no memory leaks (track allocation counts)
- [ ] Integration test: hot-reload the "rate-limiter" plugin with state migration — verify the token bucket state transfers
- [ ] Integration test: concurrent processing — 10 threads each sending 1000 requests through the "echo" plugin, verifying all responses are correct
- [ ] Integration test: capability denial — load "json-transform" with insufficient capabilities, verify graceful error
- [ ] Integration test: ABI version mismatch — attempt to load a plugin with wrong version, verify clear error message
- [ ] Stress test: load and unload a plugin 1000 times rapidly, verifying no resource leaks (file descriptors, memory)

### Build System
- [ ] Workspace with crates: `plugin-abi`, `host`, `plugin-echo`, `plugin-json-transform`, `plugin-rate-limiter`
- [ ] `plugin-abi` compiles as a regular `lib` (not cdylib) — it is a shared dependency
- [ ] Each plugin crate has `crate-type = ["cdylib"]` in its `Cargo.toml`
- [ ] A `justfile` or `Makefile` with targets: `build-plugins` (compiles all plugins), `build-host`, `test` (builds plugins first, then runs host tests with plugin directory set via env var), `watch` (hot-reload demo)
- [ ] The test harness sets `PLUGIN_DIR` env var pointing to the directory containing compiled `.so`/`.dylib` files
- [ ] `cargo test` for the host crate automatically builds plugin crates first (via `build.rs` or `cargo xtask`)

## Starting Points

- Study the **`libloading` crate** source code (https://github.com/nagisa/rust_libloading) — this is the standard Rust wrapper around `dlopen`/`dlsym`/`LoadLibrary`. Understand its `Library` and `Symbol` types, lifetime constraints (a `Symbol` borrows the `Library`), and platform-specific behavior. Read the `POSIX` and `Windows` backend implementations to understand what actually happens at the OS level
- Study the **`abi_stable` crate** (https://github.com/rodrimati1992/abi_stable_crates) — this is the most ambitious attempt at stable Rust ABI for plugins. It provides `#[sabi_trait]` for trait objects across FFI, `RVec`/`RString`/`ROption` for stable collections, and a plugin framework. Study its approach even if you build something simpler — understanding what problems it solves reveals the pitfalls you will face
- Read **"The Rust FFI Omnibus"** (http://jakegoulding.com/rust-ffi-omnibus/) — a practical guide to every FFI pattern in Rust. The sections on strings, callbacks, and objects are directly relevant
- Study the **Bevy game engine's dynamic plugin system** (https://github.com/bevyengine/bevy/tree/main/crates/bevy_dynamic_plugin) — Bevy faced the exact same problem and their solution shows practical tradeoffs. Note how they handle the `TypeId` problem (Rust type IDs are not stable across compilations)
- Read **"Plugins in Rust"** by Michael Bryan (https://michael-f-bryan.github.io/rust-ffi-guide/) — specifically the chapter on dynamic loading, which walks through the entire pattern from dlopen to trait objects across FFI
- Study **HashiCorp's `go-plugin`** design (https://github.com/hashicorp/go-plugin) — while in Go, this is the industry standard for plugin systems. Its design (process isolation, gRPC-based communication, health checks, versioning) represents the production baseline you should aim to match in capability, even if your mechanism (shared libraries) differs
- Read the **`dlopen` man page** and understand `RTLD_NOW` vs `RTLD_LAZY`, `RTLD_LOCAL` vs `RTLD_GLOBAL`, and the implications for symbol resolution. `RTLD_NOW` is essential for fail-fast on missing symbols; `RTLD_LOCAL` prevents plugins from interfering with each other's symbols
- Study the **`notify` crate** (https://github.com/notify-rs/notify) for filesystem watching — you will use this for hot-reload. Understand its debouncing configuration (critical for avoiding double-load when a compiler writes a file in stages) and cross-platform event types
- Read the Rust **Nomicon chapter on FFI** (https://doc.rust-lang.org/nomicon/ffi.html) — especially the sections on `repr(C)`, calling conventions, and unwind safety across FFI boundaries. The section on `catch_unwind` at FFI boundaries is mandatory reading

## Hints

1. Start with the absolute minimum: a single plugin with one function `fn process(input: i32) -> i32`, loaded via `libloading`. Get this compiling and running on your platform before adding any complexity. The number of things that can go wrong with dynamic loading (wrong paths, symbol name mangling, library search paths, platform differences) means you want a working foundation first
2. The `#[no_mangle]` attribute and `extern "C"` are both required for symbols to be discoverable via `dlsym`. Rust's default symbol mangling produces names like `_ZN7my_crate8process17hf3e8a0b1c2d3e4f5E` which cannot be found. `#[no_mangle] pub extern "C" fn` gives you a clean `process` symbol
3. For the `PluginDeclaration` pattern, have each plugin export a single `_plugin_declaration` function that returns a struct of function pointers. This is safer than resolving multiple symbols individually — you get all-or-nothing loading and a single version check
4. The `HostApi` struct of function pointers is the capability boundary. A function pointer that is null or points to a deny-stub is a capability that was not granted. This is more secure than checking a capability bitflag at runtime because the plugin literally cannot call the function — there is no code path to execute
5. For `#[repr(C)]` types, remember: no `enum` with data (use tagged unions manually), no `String` (use `*const u8` + `usize`), no `Vec` (use `*const T` + `usize`), no `bool` (use `u8` with 0/1), no `Option` (use sentinel values or a `is_present: u8` flag). The `plugin_abi` crate should provide conversion functions for all of these
6. The `PluginContext` is an opaque pointer from the host's perspective — only the plugin knows its layout. The plugin allocates it in `init`, uses it in `process` (via `&mut *ctx`), and frees it in `shutdown`. The host merely passes the pointer through. Use `Box::into_raw` to create it and `Box::from_raw` to free it
7. For hot-reloading, the critical sequence is: (a) load new library, (b) create new context, (c) call migrate, (d) swap the `Arc<LoadedPlugin>` atomically, (e) wait for old reference count to reach zero, (f) shutdown old context, (g) close old library. An `Arc<RwLock<LoadedPlugin>>` or `arc-swap` crate makes the atomic swap clean
8. The file watcher will fire multiple events for a single recompile (create temp file, write, rename). Use the `notify` crate's debouncing feature with a 500ms delay to coalesce these into a single reload event. Also verify the file is a valid shared library before attempting to load it
9. For thread safety in `process`, either: (a) the plugin's `process` function is inherently thread-safe (all state in `PluginContext` is behind `Mutex`), and the host calls it from any thread, or (b) the host serializes calls to each plugin instance. Option (a) is more performant but requires plugins to be thread-safe — document this requirement clearly in the ABI
10. `std::panic::catch_unwind` at every FFI boundary is non-negotiable. A panic that unwinds across `extern "C"` is undefined behavior. Wrap every plugin callback: `let result = std::panic::catch_unwind(|| { /* plugin code */ }); match result { Ok(v) => v, Err(_) => ERROR_PLUGIN_PANICKED }`
11. For the memory protocol, the simplest correct approach is "caller allocates, caller frees": the host allocates the `Request` buffer, passes it to the plugin, the plugin reads it. For the response, the host allocates a buffer of some initial capacity, passes `(buffer_ptr, capacity)` to the plugin, the plugin writes and returns actual length. If `actual_length > capacity`, the host reallocates and retries. This avoids all cross-boundary allocation issues
12. `TypeId` is NOT stable across compilations — two cdylib crates compiled separately will have different `TypeId` values for the same type. Never use `TypeId` or `Any::downcast` across the plugin boundary. Use explicit type tags (enum discriminants, magic numbers) instead
13. For IPC between plugins, the host maintains a `HashMap<String, crossbeam::channel::Sender<Vec<u8>>>` where each plugin gets a receiver during `init`. The `ipc_send` host function looks up the target plugin's sender and forwards the message. Keep messages as raw bytes — let plugins serialize/deserialize their own protocol
14. To test hot-reloading programmatically: compile the plugin once, copy the `.so` to the plugin directory, load it, verify behavior, recompile the plugin with different behavior (e.g., different return value), copy the new `.so` over the old one, wait for the watcher to trigger, verify the new behavior. Use a test-specific plugin that reads its behavior from an environment variable or config file
15. AddressSanitizer testing requires compiling both the host AND the plugins with sanitizer flags. Set `RUSTFLAGS="-Zsanitizer=address"` globally for the workspace build. Note that ASan may report leaks from `dlopen` itself (the OS caches loaded libraries) — add known suppressions as needed
16. For the "length-prefixed double-call" protocol mentioned in the memory criteria: first call is `process(ctx, request, null, 0, &mut required_size)` which returns `BUFFER_TOO_SMALL` and sets `required_size`. Second call is `process(ctx, request, buffer, required_size, &mut actual_size)` which fills the buffer. This pattern is used throughout the Windows API and is battle-tested

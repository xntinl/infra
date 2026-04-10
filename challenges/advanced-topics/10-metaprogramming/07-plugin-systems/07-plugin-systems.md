<!--
type: reference
difficulty: advanced
section: [10-metaprogramming]
concepts: [go-plugin, dynamic-linking, libloading, no_mangle, wasm-plugins, wasmtime, wazero, plugin-safety]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: create
prerequisites: [shared-libraries, dynamic-linking-basics, cgo-basics, rust-unsafe-basics, webassembly-basics]
papers: []
industry_use: [hashicorp-go-plugin, vscode-extensions, wasm-plugins, kubectl-plugins, terraform-providers]
language_contrast: high
-->

# Plugin Systems

> Loading code at runtime that was not present at compile time — the hardest form of metaprogramming, where the safety guarantees of both languages break down at the boundary.

## Mental Model

A plugin system lets a program load and execute code that was compiled separately and that the host program has never seen. This is the enabling technology for extensible applications: VS Code extensions, Kubernetes admission webhooks, Terraform providers, kubectl plugins, HashiCorp Vault secrets engines.

The fundamental challenge is the boundary. Inside your program, the compiler enforces memory safety, type correctness, and the absence of undefined behavior. At the plugin boundary, you cross into territory where you are calling code you did not compile and cannot fully trust. Every plugin system in production is a study in managing this boundary cost.

There are three viable approaches with distinct safety profiles:

1. **Native dynamic libraries** (`.so` / `.dylib` / `.dll`): maximum performance, minimum isolation. A plugin crash kills the host. ABI stability requirements are severe. Go plugins on Linux only; Rust `dylib` crates universally available but unsafe.

2. **Process isolation** (HashiCorp go-plugin model): the plugin runs in a separate OS process. The boundary is an RPC call. A plugin crash does not kill the host. The cost is IPC overhead (~100µs per call vs ~10ns for a function call).

3. **WebAssembly sandbox**: the plugin runs in a WASM virtual machine. Full memory isolation. A plugin cannot access the host's memory or file system beyond what the host explicitly exposes. The cost is 10-100x overhead vs native code (improving with WASM JIT), plus the constraint that plugins must be compiled to WASM target.

The production trend is toward WASM plugins for new systems (VS Code's extension isolation model, Envoy's extensibility, HashiCorp is moving in this direction) because the safety guarantees are too valuable to give up. Native plugins remain in legacy systems and hot paths where the 100x overhead is unacceptable.

## Core Concepts

### Go Native Plugins (`plugin.Open`)

Go's `plugin` package (`go/src/plugin`) loads `.so` files at runtime:

```go
p, err := plugin.Open("./myplugin.so")
sym, err := p.Lookup("PluginFunc")
fn := sym.(func(string) string)
result := fn("input")
```

Severe constraints:
- **Linux only** (macOS has limited support in recent versions; Windows: none)
- The plugin and host must be compiled with the **exact same Go version**
- All shared dependencies (standard library, third-party packages) must be at the **same version**
- CGO must be enabled
- Plugins cannot be unloaded once loaded
- Init functions in the plugin run when the plugin is loaded

These constraints make Go plugins unusable in most practical scenarios. The Go team has acknowledged the limitations. In production Go codebases, almost everyone uses the HashiCorp go-plugin approach instead.

### Rust `libloading` for Dynamic Libraries

Rust does not have a native plugin API. Instead, native plugins use the OS dynamic library system via the `libloading` crate:

```rust
use libloading::{Library, Symbol};

unsafe {
    let lib = Library::new("./libplugin.so")?;
    let func: Symbol<unsafe extern fn(i32) -> i32> = lib.get(b"plugin_func")?;
    let result = func(42);
}
```

The plugin must export symbols with C ABI (`extern "C"`) and use `#[no_mangle]` to prevent name mangling:

```rust
// In the plugin crate (compiled as cdylib)
#[no_mangle]
pub extern "C" fn plugin_func(x: i32) -> i32 {
    x * 2
}
```

Everything across the boundary is `unsafe`. Type mismatches (calling a `fn(i32)` as `fn(i64)`) are undefined behavior. Passing Rust types (structs with Drop impls, Vec, String) across the boundary requires careful ABI considerations — use only C-compatible types (`i32`, `f64`, raw pointers, `#[repr(C)]` structs).

### HashiCorp go-plugin: Process Isolation via RPC

HashiCorp go-plugin starts each plugin as a subprocess and communicates via gRPC over a local socket. The interface looks like a local function call to the host, but crosses an OS process boundary:

```go
// Host side
client := plugin.NewClient(&plugin.ClientConfig{
    Plugins: map[string]plugin.Plugin{
        "greeter": &GreeterPlugin{},
    },
    Cmd: exec.Command("./plugin-binary"),
})

rpcClient, _ := client.Client()
raw, _ := rpcClient.Dispense("greeter")
greeter := raw.(Greeter)
fmt.Println(greeter.Greet("world")) // RPC call under the hood
```

This is the architecture behind every HashiCorp product's extensibility: Terraform providers, Vault secrets engines, Nomad task drivers, Consul mesh gateways. The plugin crash isolation is the critical property.

### WebAssembly Plugins

WASM plugins compile the plugin to `wasm32-wasi` target and execute them in a WASM runtime embedded in the host. The host explicitly controls what the plugin can see (filesystem paths, env vars, network access) through capability-based permissions.

**Rust WASM plugin host (`wasmtime`):**
```rust
use wasmtime::*;
use wasmtime_wasi::WasiCtxBuilder;

let engine = Engine::default();
let module = Module::from_file(&engine, "plugin.wasm")?;
let wasi = WasiCtxBuilder::new().inherit_stdio().build();
let mut store = Store::new(&engine, wasi);
// link WASI functions, then instantiate
let instance = Instance::new(&mut store, &module, &[])?;
let func = instance.get_typed_func::<(i32,), (i32,)>(&mut store, "plugin_func")?;
let result = func.call(&mut store, (42,))?;
```

**Go WASM plugin host (`wazero`):**
```go
r := wazero.NewRuntime(ctx)
code, _ := os.ReadFile("plugin.wasm")
mod, _ := r.Instantiate(ctx, code)
fn := mod.ExportedFunction("plugin_func")
results, _ := fn.Call(ctx, 42)
```

## Implementation: Go

### HashiCorp go-plugin Pattern (Process Isolation)

This is the production-grade approach. The example shows the interface definition, plugin implementation, and host loading code.

```go
// shared/interface.go — shared between host and all plugins
package shared

// Greeter is the interface that all greeter plugins must implement.
// This is compiled into both the host and the plugin binary.
type Greeter interface {
    Greet(name string) (string, error)
}

// HandshakeConfig is used to verify the plugin is the expected type.
// Version mismatch causes the client to reject the plugin immediately.
var HandshakeConfig = plugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   "GREETER_PLUGIN",
    MagicCookieValue: "hello",
}
```

```go
// plugin/main.go — the plugin binary (compiled separately)
package main

import (
    "fmt"
    "os"
    "github.com/hashicorp/go-plugin"
    "myapp/shared"
)

type GreeterImpl struct{}

func (g *GreeterImpl) Greet(name string) (string, error) {
    return fmt.Sprintf("Hello, %s! (from plugin process %d)", name, os.Getpid()), nil
}

func main() {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: shared.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "greeter": &GreeterPlugin{Impl: &GreeterImpl{}},
        },
        // GRPCServer tells go-plugin to use gRPC transport
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

```go
// host/main.go — the host application
package main

import (
    "fmt"
    "log"
    "os/exec"
    "github.com/hashicorp/go-plugin"
    "myapp/shared"
)

func main() {
    client := plugin.NewClient(&plugin.ClientConfig{
        HandshakeConfig: shared.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "greeter": &shared.GreeterGRPCPlugin{},
        },
        Cmd:              exec.Command("./greeter-plugin"),
        AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
    })
    defer client.Kill() // ensures the subprocess is cleaned up

    grpcClient, err := client.Client()
    if err != nil {
        log.Fatalf("connect: %v", err)
    }

    raw, err := grpcClient.Dispense("greeter")
    if err != nil {
        log.Fatalf("dispense: %v", err)
    }

    greeter := raw.(shared.Greeter)
    msg, err := greeter.Greet("world")
    if err != nil {
        log.Fatalf("greet: %v", err)
    }
    fmt.Println(msg)
}
```

### wazero WASM Plugin Host

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/tetratelabs/wazero"
    "github.com/tetratelabs/wazero/api"
    "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func main() {
    ctx := context.Background()

    // wazero has zero CGO dependencies — pure Go WASM runtime
    r := wazero.NewRuntime(ctx)
    defer r.Close(ctx)

    // Instantiate WASI so plugins can do I/O
    wasi_snapshot_preview1.MustInstantiate(ctx, r)

    // Load the WASM plugin binary
    pluginBytes, err := os.ReadFile("plugin.wasm")
    if err != nil {
        log.Fatalf("read plugin: %v", err)
    }

    // Instantiate the plugin — this runs the plugin's _start or init
    mod, err := r.InstantiateWithConfig(ctx, pluginBytes,
        wazero.NewModuleConfig().WithStdout(os.Stdout).WithStderr(os.Stderr))
    if err != nil {
        log.Fatalf("instantiate: %v", err)
    }

    // Call an exported function from the WASM plugin
    // The plugin must export this function with wasm32-wasi target:
    // #[no_mangle] pub extern "C" fn double(x: i32) -> i32 { x * 2 }
    double := mod.ExportedFunction("double")
    if double == nil {
        log.Fatal("plugin does not export 'double'")
    }

    results, err := double.Call(ctx, api.EncodeI32(21))
    if err != nil {
        log.Fatalf("call double: %v", err)
    }
    fmt.Printf("double(21) = %d\n", api.DecodeI32(results[0])) // 42
}
```

### Go-specific considerations

**The go-plugin architecture is the de facto standard in Go**: Terraform, Vault, Consul, Nomad, and most serious Go plugin systems use it. The subprocess model solves the critical problems (crash isolation, version independence) at the cost of IPC overhead. For Terraform providers (which run once per apply and make dozens of API calls), this overhead is completely acceptable.

**CGO is required for Go native plugins**: `plugin.Open` requires CGO. If your deployment requires CGO-free binaries (common for minimal Docker images, WASM targets), native plugins are not an option. go-plugin works without CGO (it is pure gRPC). wazero is specifically designed to be CGO-free.

## Implementation: Rust

### Native Dylib Plugin with `libloading`

```rust
// In the plugin crate (Cargo.toml: [lib] crate-type = ["cdylib"])

use std::ffi::{CStr, CString};
use std::os::raw::c_char;

/// Plugin metadata — returned by all plugins to identify themselves.
#[repr(C)]
pub struct PluginInfo {
    pub name: *const c_char,
    pub version: *const c_char,
}

/// Exported plugin entry point — must use C ABI and no name mangling.
#[no_mangle]
pub extern "C" fn plugin_info() -> PluginInfo {
    PluginInfo {
        name: b"my-plugin\0".as_ptr() as *const c_char,
        version: b"1.0.0\0".as_ptr() as *const c_char,
    }
}

/// A simple computation exported from the plugin.
#[no_mangle]
pub extern "C" fn transform(value: i64) -> i64 {
    value * value + value + 41
}

/// Example with string passing — callers must free the returned string.
/// This is the standard C-style string ownership convention across FFI.
#[no_mangle]
pub extern "C" fn format_value(value: i64) -> *mut c_char {
    let s = format!("value={} squared+value+41={}", value, value * value + value + 41);
    // CString::into_raw transfers ownership to the caller
    CString::new(s).unwrap().into_raw()
}

/// Must be called by the host to free strings returned by format_value.
#[no_mangle]
pub extern "C" fn free_string(ptr: *mut c_char) {
    if ptr.is_null() { return; }
    // Safety: this pointer was created by CString::into_raw in this crate
    unsafe { drop(CString::from_raw(ptr)); }
}
```

```rust
// In the host crate (Cargo.toml: [dependencies] libloading = "0.8")

use libloading::{Library, Symbol};
use std::ffi::CStr;

#[repr(C)]
struct PluginInfo {
    name: *const std::os::raw::c_char,
    version: *const std::os::raw::c_char,
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Safety: We are calling into a dynamic library.
    // Invariants we must uphold:
    // 1. The function signatures match what the plugin exports.
    // 2. We do not call these functions after the Library is dropped.
    // 3. Strings returned by format_value are freed by free_string.
    unsafe {
        let lib = Library::new("./libmy_plugin.so")?;

        // Look up the plugin_info symbol
        let plugin_info_fn: Symbol<unsafe extern fn() -> PluginInfo> =
            lib.get(b"plugin_info")?;
        let info = plugin_info_fn();
        let name = CStr::from_ptr(info.name).to_str()?;
        let version = CStr::from_ptr(info.version).to_str()?;
        println!("Loaded plugin: {} v{}", name, version);

        // Call the transform function
        let transform: Symbol<unsafe extern fn(i64) -> i64> =
            lib.get(b"transform")?;
        println!("transform(10) = {}", transform(10));

        // Call the string-returning function
        let format_value: Symbol<unsafe extern fn(i64) -> *mut std::os::raw::c_char> =
            lib.get(b"format_value")?;
        let free_string: Symbol<unsafe extern fn(*mut std::os::raw::c_char)> =
            lib.get(b"free_string")?;

        let ptr = format_value(10);
        let result = CStr::from_ptr(ptr).to_str()?.to_owned();
        free_string(ptr); // must free before returning
        println!("{}", result);
    }

    Ok(())
}
```

### Rust WASM Plugin

```rust
// Plugin source compiled to wasm32-wasi
// Cargo.toml: [lib] crate-type = ["cdylib"]

/// Double a value — exported for the WASM host to call.
#[no_mangle]
pub extern "C" fn double(x: i32) -> i32 {
    x * 2
}

/// Process a string via WASM's linear memory.
/// The host writes the string to memory at `ptr` with `len` bytes.
/// This function returns a pointer to the result string.
/// The host reads from that pointer until it finds a null byte.
#[no_mangle]
pub extern "C" fn reverse_string(ptr: *mut u8, len: usize) -> *mut u8 {
    // Safety: host is responsible for keeping this memory valid
    let slice = unsafe { std::slice::from_raw_parts(ptr, len) };
    let mut result: Vec<u8> = slice.iter().rev().cloned().collect();
    result.push(0); // null terminator
    let out_ptr = result.as_mut_ptr();
    std::mem::forget(result); // host is responsible for freeing
    out_ptr
}
```

### Rust-specific considerations

**`#[no_mangle]` and `extern "C"` are the minimum requirements for a plugin export.** Without `#[no_mangle]`, `rustc` generates a mangled symbol name that encodes the crate and module path — the host cannot find it by the simple function name. Without `extern "C"`, the function uses the Rust calling convention, which is unstable and may differ between compiler versions.

**`crate-type = ["cdylib"]` vs `["dylib"]`**: A `cdylib` is a C-compatible dynamic library that does not export Rust symbols or the Rust standard library (it links them statically). A `dylib` is a Rust dynamic library that exposes Rust symbols and is intended for Rust-to-Rust dynamic linking. In practice, `cdylib` is almost always what you want for a plugin — it minimizes ABI surface and does not require the consumer to use the same Rust version.

**The stability trap**: Rust has no stable ABI. Any change to a data structure definition (adding a field, reordering fields, changing alignment) breaks binary compatibility with existing plugins. For production plugin systems, you must either: (a) use only C primitive types and `#[repr(C)]` structs across the boundary; (b) version the plugin protocol and support multiple versions; or (c) use the WASM approach to avoid ABI entirely.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Native plugin mechanism | `plugin.Open` + `.so` (Linux only) | `libloading` crate + `extern "C"` |
| Plugin safety | Unsafe — crash kills host | Unsafe — crash kills host, UB on type mismatch |
| Platform support | Linux only (Go native plugins) | Universal (`libloading` wraps OS dlopen) |
| Process isolation model | HashiCorp go-plugin (gRPC subprocess) | No stdlib equivalent; implement with std::process |
| WASM plugin runtime | `wazero` (zero CGO, pure Go) | `wasmtime` crate (CGO-based WASM JIT) |
| ABI stability | Go native plugins require identical Go version | Rust has no stable ABI; use C types only |
| Plugin unloading | Not supported | `libloading::Library::close()` (UB-prone) |
| Production recommendation | go-plugin (subprocess) or wazero (WASM) | WASM (wasmtime) or process isolation |

## Production War Stories

**HashiCorp Terraform providers**: Every Terraform provider (AWS, GCP, Azure, Kubernetes...) is a separate binary that Terraform forks as a subprocess on `terraform init`. This is the go-plugin architecture. It means a buggy AWS provider cannot crash the Terraform core process, and providers can be written in any language that speaks gRPC. The cost: initialization latency (each provider starts a subprocess and negotiates a gRPC connection). For large Terraform configurations with 20+ providers, this adds a few seconds to `terraform plan`. The Terraform team has mitigated this with a persistent provider daemon mode.

**VS Code extensions and the WASM pivot**: Early VS Code extensions ran in a shared Node.js process. A buggy extension could freeze the entire editor. VS Code's extension host isolation moved extensions to a separate process — the go-plugin pattern for Node.js. The WASM approach (adopted for VS Code for the Web) is the logical conclusion: the extension runs in a sandboxed WASM environment with no ability to access the host filesystem or network beyond what VS Code explicitly grants.

**Go native plugin failure in practice**: The Go team introduced native plugins in Go 1.8 (2017). Despite being in the standard library, they have seen almost no production adoption outside of CGO-heavy applications. The reasons: Linux-only, same Go version requirement, no unload support, and the lack of ABI stability all combine to make them impractical for the main use case (extensible applications). The HashiCorp go-plugin approach appeared before native plugins and solved the problem better. The native plugin mechanism remains a niche tool.

**`extism`: the portable WASM plugin standard**: Extism is an open-source plugin system that standardizes the WASM plugin interface for multiple host languages (Go, Rust, Python, Ruby, etc.). A plugin written as a WASM module can be loaded by any Extism host. This is the direction the industry is moving: WASM as the universal plugin bytecode format, with the host responsible for capability grants.

## Complexity Analysis

| Dimension | Native Plugin | Process (go-plugin) | WASM Plugin |
|-----------|--------------|---------------------|-------------|
| Call overhead | ~10 ns (function call) | ~100 µs (IPC + gRPC) | ~1-10 µs (WASM JIT) |
| Isolation | None (shared memory) | Full (separate process) | Full (sandboxed VM) |
| Plugin crash impact | Host crash | Host survives | Host survives |
| Plugin unloading | Not supported in Go | Kill subprocess | Unload WASM instance |
| Memory overhead | None | ~10 MB per subprocess | ~5-20 MB per instance |
| Development complexity | Low | Medium (gRPC schema) | Medium (WASM target) |
| Security | No sandbox | OS process boundary | Capability-based sandbox |

## Common Pitfalls

**1. Passing Rust or Go types across a native plugin boundary.** `Vec<T>`, `String`, `Box<T>`, Go slices, Go interfaces — these types have internal pointers that are valid only in the process that allocated them. Passing them to a separately compiled plugin is undefined behavior. Use only C primitive types, raw pointers, and `#[repr(C)]` structs at the boundary.

**2. Not handling plugin crashes.** If you are using native plugins and the plugin panics, your host process panics too. If you need crash isolation, use the subprocess model. If using gRPC-based go-plugin, always handle `rpc error` returns — they indicate the plugin subprocess died.

**3. Not versioning the plugin protocol.** Plugin systems that do not version their interface eventually break when a new plugin talks to an old host or vice versa. Always include a version negotiation step. go-plugin's `HandshakeConfig.ProtocolVersion` does this. For WASM plugins, version the exported function names.

**4. Forgetting to free memory across a native FFI boundary.** If the plugin allocates memory and returns a pointer to the host, ownership of that memory must be explicitly defined and followed. The pattern in the example above (`format_value` + `free_string`) is the standard C convention: the allocator is responsible for providing a free function. Forgetting to call the free function is a memory leak. Calling the wrong free function is undefined behavior.

**5. Linking order issues with `libloading`.** On some Linux configurations, loading a `.so` that was compiled without `-fPIC` or with incompatible linker settings causes subtle memory corruption or immediate SIGSEGV. Always compile plugins with `cargo build --release` for consistent settings, and test loading on the target platform before shipping.

## Exercises

**Exercise 1** (30 min): Write a Go program that uses `plugin.Open` to load a simple `.so` plugin and call an exported `func Add(a, b int) int` function. Build the plugin as a separate `go build -buildmode=plugin`. Verify the result is correct. Document what happens if you try to load the plugin built with a different Go version.

**Exercise 2** (2-4h): Implement the HashiCorp go-plugin pattern manually (without using the `go-plugin` library): define an interface, implement it in a plugin binary that starts an HTTP/JSON-RPC server on a local socket, and implement a client in the host that connects to the plugin process, calls the interface methods via HTTP, and handles the subprocess lifecycle (start, kill, restart on crash). Compare this to the gRPC-based approach.

**Exercise 3** (4-8h): Build a Rust WASM plugin system using `wasmtime`. Define a plugin interface: the WASM module must export `init() -> i32` (returns 0 on success), `process(ptr: i32, len: i32) -> i32` (processes a byte buffer, returns result length), and the host provides a `host_log(ptr: i32, len: i32)` import for plugin logging. Write a host that loads the plugin, calls `init`, passes a byte buffer, and reads the result. Write a Rust WASM plugin that implements the interface. Test that a plugin that calls `process` with invalid data does not crash the host.

**Exercise 4** (8-15h): Implement a complete plugin system for a simple text transformation pipeline: (a) define a plugin trait (`Transform`) in a shared crate with `name() -> &str` and `transform(input: &str) -> String`; (b) implement three plugins as separate crates compiled as both `cdylib` (native) and `wasm32-wasi` (WASM); (c) build a host that can load plugins from either `.so` files or `.wasm` files, selecting the loader based on file extension; (d) add a plugin registry that discovers all plugins in a directory, loads them, and applies them in sequence to an input string. Benchmark the native vs WASM loading overhead with `criterion`.

## Further Reading

### Foundational Papers

- [Saltzer & Schroeder: "The Protection of Information in Computer Systems" (1975)](https://web.mit.edu/Saltzer/www/publications/protection.html) — the foundational paper on capability-based security; the model underlying WASM plugin systems.

### Books

- [The WebAssembly Specification](https://webassembly.github.io/spec/core/) — the formal definition; chapters on the module system explain the import/export model that WASM plugins use.
- [Programming WebAssembly with Rust (Kevin Hoffman)](https://pragprog.com/titles/khrust/programming-webassembly-with-rust/) — practical WASM development from Rust.

### Production Code to Read

- [`hashicorp/go-plugin`](https://github.com/hashicorp/go-plugin) — the canonical Go plugin framework; study `client.go` and `server.go` for the subprocess lifecycle.
- [`wasmtime` examples](https://github.com/bytecodealliance/wasmtime/tree/main/examples) — Rust host examples; `hello.rs` is the minimal starting point.
- [`wazero` examples](https://github.com/tetratelabs/wazero/tree/main/examples) — Go host examples; zero CGO is the key differentiator.
- [`extism`](https://github.com/extism/extism) — the portable WASM plugin SDK for multiple languages; study the PDK (Plugin Development Kit) design.
- [`libloading` crate](https://github.com/nicowillis/libloading) — the Rust dynamic library loader; `src/lib.rs` for the safety model.

### Talks

- [Lin Clark: "WebAssembly Interface Types" (Strange Loop 2019)](https://www.youtube.com/watch?v=Qn_4F3foB3Q) — the direction WASM is heading for plugin systems.
- [Mitchell Hashimoto: "go-plugin" (GopherCon 2019)](https://www.youtube.com/watch?v=0ORTXKbpW1Y) — the author explaining the design and tradeoffs.

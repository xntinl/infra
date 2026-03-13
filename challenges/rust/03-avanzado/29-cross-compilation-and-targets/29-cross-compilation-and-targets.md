# 29. Cross-Compilation and Targets

**Dificultad**: Avanzado

## Objetivo

Dominar la compilacion cruzada en Rust: gestionar targets con rustup, configurar linkers, compilar para multiples arquitecturas y sistemas operativos, generar binarios estaticos con musl, compilar a WebAssembly, y establecer matrices de CI para builds multi-target. Al final tendras un proyecto que compila para Linux x86_64, ARM64, macOS y WASM desde una sola maquina.

---

## 1. Target Triples: Anatomia

Un target triple en Rust sigue el formato:

```
<arch>-<vendor>-<os>-<env>
```

Ejemplos concretos:

| Triple | Descripcion |
|---|---|
| `x86_64-unknown-linux-gnu` | Linux 64-bit con glibc |
| `x86_64-unknown-linux-musl` | Linux 64-bit enlazado estaticamente (musl) |
| `aarch64-unknown-linux-gnu` | Linux ARM64 (Raspberry Pi 4, Graviton) |
| `x86_64-apple-darwin` | macOS Intel |
| `aarch64-apple-darwin` | macOS Apple Silicon |
| `wasm32-unknown-unknown` | WebAssembly sin runtime especifico |
| `wasm32-wasi` | WebAssembly con WASI |
| `x86_64-pc-windows-msvc` | Windows 64-bit con MSVC |

Consulta todos los targets disponibles:

```bash
rustup target list
```

Filtra los que ya tienes instalados:

```bash
rustup target list --installed
```

---

## 2. Instalacion de Targets

Agrega targets con rustup:

```bash
# Linux musl (binarios estaticos)
rustup target add x86_64-unknown-linux-musl

# ARM64 Linux
rustup target add aarch64-unknown-linux-gnu

# WebAssembly
rustup target add wasm32-unknown-unknown
rustup target add wasm32-wasi

# macOS (si estas en Linux y quieres cross-compilar)
rustup target add aarch64-apple-darwin
```

Verifica la instalacion:

```bash
rustup target list --installed
# Deberia mostrar los targets agregados junto con tu host target
```

---

## 3. Compilacion Basica para Otro Target

Crea un proyecto de ejemplo:

```bash
cargo new cross-demo
cd cross-demo
```

Edita `src/main.rs`:

```rust
fn main() {
    println!("Arquitectura: {}", std::env::consts::ARCH);
    println!("OS: {}", std::env::consts::OS);
    println!("Familia: {}", std::env::consts::FAMILY);

    let info = platform_info();
    println!("{info}");
}

fn platform_info() -> String {
    #[cfg(target_os = "linux")]
    {
        return "Ejecutando en Linux".to_string();
    }

    #[cfg(target_os = "macos")]
    {
        return "Ejecutando en macOS".to_string();
    }

    #[cfg(target_os = "windows")]
    {
        return "Ejecutando en Windows".to_string();
    }

    #[cfg(target_arch = "wasm32")]
    {
        return "Ejecutando en WebAssembly".to_string();
    }

    #[allow(unreachable_code)]
    "Plataforma desconocida".to_string()
}
```

Compila para musl (binario estatico):

```bash
cargo build --target x86_64-unknown-linux-musl --release
```

Verifica que el binario es estatico:

```bash
file target/x86_64-unknown-linux-musl/release/cross-demo
# Deberia decir "statically linked"

ldd target/x86_64-unknown-linux-musl/release/cross-demo
# Deberia decir "not a dynamic executable"
```

---

## 4. Compilacion Condicional Avanzada

Rust ofrece atributos `cfg` para controlar que codigo se compila segun la plataforma:

```rust
// Modulos completos condicionales
#[cfg(target_os = "linux")]
mod linux_specific;

#[cfg(target_os = "macos")]
mod macos_specific;

// Funciones condicionales
#[cfg(target_arch = "x86_64")]
fn optimized_operation(data: &[u8]) -> u64 {
    // Podria usar SIMD especifico de x86_64
    data.iter().map(|&b| b as u64).sum()
}

#[cfg(target_arch = "aarch64")]
fn optimized_operation(data: &[u8]) -> u64 {
    // Implementacion para ARM64
    data.iter().map(|&b| b as u64).sum()
}

#[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
fn optimized_operation(data: &[u8]) -> u64 {
    // Fallback generico
    data.iter().map(|&b| b as u64).sum()
}
```

Combinaciones logicas con `cfg`:

```rust
// AND: ambas condiciones deben cumplirse
#[cfg(all(target_os = "linux", target_arch = "x86_64"))]
fn linux_x86_only() {
    println!("Solo en Linux x86_64");
}

// OR: cualquiera de las condiciones
#[cfg(any(target_os = "linux", target_os = "macos"))]
fn unix_like() {
    println!("Linux o macOS");
}

// NOT: negacion
#[cfg(not(target_os = "windows"))]
fn not_windows() {
    println!("Cualquier cosa menos Windows");
}

// cfg_if! para cadenas complejas (crate cfg-if)
// En Cargo.toml: cfg-if = "1"
cfg_if::cfg_if! {
    if #[cfg(target_os = "linux")] {
        fn platform_socket() -> &'static str { "epoll" }
    } else if #[cfg(target_os = "macos")] {
        fn platform_socket() -> &'static str { "kqueue" }
    } else if #[cfg(target_os = "windows")] {
        fn platform_socket() -> &'static str { "iocp" }
    } else {
        fn platform_socket() -> &'static str { "poll" }
    }
}
```

Usa `cfg` en tiempo de ejecucion con `cfg!()` (macro, no atributo):

```rust
fn check_platform() {
    if cfg!(target_os = "linux") {
        println!("Estamos en Linux");
    }

    // Esto siempre compila, pero el branch inactivo se elimina
    // por el compilador (evaluacion en compilacion)
    let pointer_width = if cfg!(target_pointer_width = "64") {
        "64-bit"
    } else {
        "32-bit"
    };
    println!("Ancho de puntero: {pointer_width}");
}
```

---

## 5. Feature Flags para Codigo de Plataforma

En lugar de `cfg` directo, usa features para abstraer plataformas:

```toml
# Cargo.toml
[package]
name = "cross-demo"
version = "0.1.0"
edition = "2024"

[features]
default = ["native"]
native = []
web = ["dep:wasm-bindgen"]

[dependencies]
cfg-if = "1"

[target.'cfg(target_arch = "wasm32")'.dependencies]
wasm-bindgen = { version = "0.2", optional = true }

[target.'cfg(not(target_arch = "wasm32"))'.dependencies]
tokio = { version = "1", features = ["full"] }
```

Codigo que usa features + cfg combinados:

```rust
#[cfg(feature = "native")]
mod native_runtime {
    pub fn start() {
        let rt = tokio::runtime::Runtime::new().unwrap();
        rt.block_on(async {
            println!("Runtime nativo con tokio");
        });
    }
}

#[cfg(feature = "web")]
mod web_runtime {
    use wasm_bindgen::prelude::*;

    #[wasm_bindgen]
    pub fn greet(name: &str) -> String {
        format!("Hola desde WASM, {name}!")
    }
}

// Dependencias condicionales por target en Cargo.toml
// [target.'cfg(target_os = "linux")'.dependencies]
// nix = "0.27"
//
// [target.'cfg(target_os = "windows")'.dependencies]
// windows = "0.52"
```

---

## 6. Configuracion de Linkers

Cuando compilas para un target diferente al host, necesitas un linker compatible. Configura en `.cargo/config.toml`:

```toml
# .cargo/config.toml

# Linux ARM64 - necesita el toolchain de cross-compilacion
[target.aarch64-unknown-linux-gnu]
linker = "aarch64-linux-gnu-gcc"

# Linux musl - usa musl-gcc si esta disponible
[target.x86_64-unknown-linux-musl]
linker = "musl-gcc"
# Si musl-gcc no esta disponible, rust puede usar su linker integrado:
# rustflags = ["-C", "link-self-contained=yes"]

# WASM no necesita linker externo (usa lld integrado)
[target.wasm32-unknown-unknown]
runner = "wasm-bindgen-test-runner"

# Variables de entorno para el linker
[env]
# CC_aarch64_unknown_linux_gnu = "aarch64-linux-gnu-gcc"
```

Instalacion de toolchains de cross-compilacion en distintos OS:

```bash
# Ubuntu/Debian - ARM64 cross-compiler
sudo apt install gcc-aarch64-linux-gnu

# Ubuntu/Debian - musl tools
sudo apt install musl-tools

# macOS con Homebrew
brew install filosottile/musl-cross/musl-cross

# Verificar que el linker esta disponible
which aarch64-linux-gnu-gcc
which musl-gcc
```

Flags de compilacion por target:

```toml
# .cargo/config.toml

[target.x86_64-unknown-linux-musl]
rustflags = [
    "-C", "link-self-contained=yes",
    "-C", "target-feature=+crt-static",
]

[target.x86_64-unknown-linux-gnu]
rustflags = [
    "-C", "link-arg=-Wl,--as-needed",
]
```

---

## 7. Cross: Cross-Compilacion Simplificada

El crate `cross` usa Docker para proporcionar entornos de compilacion preconfigurados:

```bash
cargo install cross

# Compilar para ARM64 Linux (sin configurar linkers manualmente)
cross build --target aarch64-unknown-linux-gnu --release

# Compilar para musl
cross build --target x86_64-unknown-linux-musl --release

# Incluso ejecutar tests en el target (via QEMU)
cross test --target aarch64-unknown-linux-gnu
```

Configura `cross` con `Cross.toml`:

```toml
# Cross.toml
[build.env]
passthrough = [
    "RUST_LOG",
    "DATABASE_URL",
]

[target.aarch64-unknown-linux-gnu]
image = "ghcr.io/cross-rs/aarch64-unknown-linux-gnu:main"
# pre-build para instalar dependencias del sistema
pre-build = [
    "dpkg --add-architecture arm64",
    "apt-get update && apt-get install -y libssl-dev:arm64",
]

[target.x86_64-unknown-linux-musl]
image = "ghcr.io/cross-rs/x86_64-unknown-linux-musl:main"
pre-build = [
    "apt-get update && apt-get install -y musl-tools pkg-config libssl-dev",
]
```

---

## 8. Enlazado Estatico con musl

Los binarios enlazados estaticamente son ideales para contenedores scratch/distroless y entornos sin glibc:

```bash
# Compilar binario 100% estatico
cargo build --target x86_64-unknown-linux-musl --release

# Verificar que NO tiene dependencias dinamicas
ldd target/x86_64-unknown-linux-musl/release/cross-demo
# Salida: "not a dynamic executable" o "statically linked"

# Tamanio del binario
ls -lh target/x86_64-unknown-linux-musl/release/cross-demo
```

Dockerfile minimo con binario estatico:

```dockerfile
# Build stage
FROM rust:1.83 AS builder

RUN rustup target add x86_64-unknown-linux-musl
RUN apt-get update && apt-get install -y musl-tools

WORKDIR /app
COPY . .

RUN cargo build --target x86_64-unknown-linux-musl --release

# Runtime stage - imagen scratch (0 bytes base)
FROM scratch
COPY --from=builder /app/target/x86_64-unknown-linux-musl/release/cross-demo /app
ENTRYPOINT ["/app"]
```

Manejo de OpenSSL vs rustls para binarios estaticos:

```toml
# Cargo.toml - Usa rustls en lugar de openssl-sys para evitar
# problemas de enlazado con musl
[dependencies]
reqwest = { version = "0.12", default-features = false, features = ["rustls-tls"] }

# Si NECESITAS openssl con musl:
# openssl = { version = "0.10", features = ["vendored"] }
# Esto compila openssl desde fuente con musl
```

---

## 9. Compilacion a WebAssembly

```bash
rustup target add wasm32-unknown-unknown
cargo install wasm-pack
```

Proyecto WASM basico:

```rust
// src/lib.rs
use wasm_bindgen::prelude::*;

#[wasm_bindgen]
pub struct Calculator {
    history: Vec<f64>,
}

#[wasm_bindgen]
impl Calculator {
    #[wasm_bindgen(constructor)]
    pub fn new() -> Self {
        Self {
            history: Vec::new(),
        }
    }

    pub fn add(&mut self, a: f64, b: f64) -> f64 {
        let result = a + b;
        self.history.push(result);
        result
    }

    pub fn last_result(&self) -> Option<f64> {
        self.history.last().copied()
    }

    pub fn history_len(&self) -> usize {
        self.history.len()
    }
}

// Funciones standalone
#[wasm_bindgen]
pub fn fibonacci(n: u32) -> u64 {
    match n {
        0 => 0,
        1 => 1,
        _ => {
            let mut a: u64 = 0;
            let mut b: u64 = 1;
            for _ in 2..=n {
                let tmp = a + b;
                a = b;
                b = tmp;
            }
            b
        }
    }
}
```

Compila y empaqueta:

```bash
# Con wasm-pack (genera bindings JS)
wasm-pack build --target web

# O directamente con cargo
cargo build --target wasm32-unknown-unknown --release

# Optimizar tamanio del .wasm
cargo install wasm-opt
wasm-opt -Oz target/wasm32-unknown-unknown/release/cross_demo.wasm -o optimized.wasm
```

---

## 10. CI Matrix para Multi-Target

GitHub Actions workflow para compilar en multiples targets:

```yaml
# .github/workflows/cross-build.yml
name: Cross-Platform Build

on:
  push:
    branches: [main]
  pull_request:

jobs:
  build:
    strategy:
      matrix:
        include:
          - target: x86_64-unknown-linux-gnu
            os: ubuntu-latest
            artifact: cross-demo
          - target: x86_64-unknown-linux-musl
            os: ubuntu-latest
            artifact: cross-demo
            use_cross: true
          - target: aarch64-unknown-linux-gnu
            os: ubuntu-latest
            artifact: cross-demo
            use_cross: true
          - target: x86_64-apple-darwin
            os: macos-13
            artifact: cross-demo
          - target: aarch64-apple-darwin
            os: macos-latest
            artifact: cross-demo
          - target: wasm32-unknown-unknown
            os: ubuntu-latest
            artifact: cross_demo.wasm

    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4

      - name: Install Rust
        uses: dtolnay/rust-toolchain@stable
        with:
          targets: ${{ matrix.target }}

      - name: Install cross
        if: matrix.use_cross
        run: cargo install cross

      - name: Build with cross
        if: matrix.use_cross
        run: cross build --target ${{ matrix.target }} --release

      - name: Build with cargo
        if: "!matrix.use_cross"
        run: cargo build --target ${{ matrix.target }} --release

      - name: Run tests
        if: "!matrix.use_cross && matrix.target != 'wasm32-unknown-unknown'"
        run: cargo test --target ${{ matrix.target }}

      - name: Run tests with cross
        if: matrix.use_cross
        run: cross test --target ${{ matrix.target }}

      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: ${{ matrix.target }}
          path: target/${{ matrix.target }}/release/${{ matrix.artifact }}

  release:
    needs: build
    if: startsWith(github.ref, 'refs/tags/')
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4

      - name: Create release
        uses: softprops/action-gh-release@v2
        with:
          files: |
            x86_64-unknown-linux-gnu/cross-demo
            x86_64-unknown-linux-musl/cross-demo
            aarch64-unknown-linux-gnu/cross-demo
            x86_64-apple-darwin/cross-demo
            aarch64-apple-darwin/cross-demo
```

---

## 11. Patron Produccion: Crate Multi-Plataforma

Estructura de un crate real que soporta multiples plataformas:

```
my-platform-crate/
  src/
    lib.rs
    platform/
      mod.rs
      linux.rs
      macos.rs
      windows.rs
      wasm.rs
```

```rust
// src/platform/mod.rs

cfg_if::cfg_if! {
    if #[cfg(target_os = "linux")] {
        mod linux;
        pub use linux::*;
    } else if #[cfg(target_os = "macos")] {
        mod macos;
        pub use macos::*;
    } else if #[cfg(target_os = "windows")] {
        mod windows;
        pub use windows::*;
    } else if #[cfg(target_arch = "wasm32")] {
        mod wasm;
        pub use wasm::*;
    } else {
        compile_error!("Plataforma no soportada");
    }
}

// Trait comun que cada plataforma implementa
pub trait PlatformOps {
    fn hostname() -> String;
    fn temp_dir() -> std::path::PathBuf;
    fn cpu_count() -> usize;
}
```

```rust
// src/platform/linux.rs
use super::PlatformOps;

pub struct Platform;

impl PlatformOps for Platform {
    fn hostname() -> String {
        std::fs::read_to_string("/etc/hostname")
            .unwrap_or_else(|_| "unknown".into())
            .trim()
            .to_string()
    }

    fn temp_dir() -> std::path::PathBuf {
        std::path::PathBuf::from("/tmp")
    }

    fn cpu_count() -> usize {
        std::thread::available_parallelism()
            .map(|n| n.get())
            .unwrap_or(1)
    }
}
```

```rust
// src/platform/macos.rs
use super::PlatformOps;

pub struct Platform;

impl PlatformOps for Platform {
    fn hostname() -> String {
        std::env::var("HOSTNAME").unwrap_or_else(|_| "unknown".into())
    }

    fn temp_dir() -> std::path::PathBuf {
        std::env::temp_dir()
    }

    fn cpu_count() -> usize {
        std::thread::available_parallelism()
            .map(|n| n.get())
            .unwrap_or(1)
    }
}
```

```rust
// src/lib.rs
mod platform;
pub use platform::{Platform, PlatformOps};

/// Funcion publica que usa la abstraccion de plataforma
pub fn system_info() -> String {
    format!(
        "Host: {}, Temp: {}, CPUs: {}",
        Platform::hostname(),
        Platform::temp_dir().display(),
        Platform::cpu_count(),
    )
}
```

---

## 12. Build Script para Deteccion de Target

Usa `build.rs` para logica condicional en tiempo de compilacion:

```rust
// build.rs
fn main() {
    let target = std::env::var("TARGET").unwrap();
    let target_os = std::env::var("CARGO_CFG_TARGET_OS").unwrap();
    let target_arch = std::env::var("CARGO_CFG_TARGET_ARCH").unwrap();

    println!("cargo:rustc-env=BUILD_TARGET={target}");
    println!("cargo:rustc-env=BUILD_TARGET_OS={target_os}");
    println!("cargo:rustc-env=BUILD_TARGET_ARCH={target_arch}");

    // Habilitar cfg flags personalizados
    if target.contains("musl") {
        println!("cargo:rustc-cfg=static_build");
    }

    if target_arch == "wasm32" {
        println!("cargo:rustc-cfg=wasm");
    }

    // Vincular librerias del sistema segun la plataforma
    if target_os == "linux" {
        println!("cargo:rustc-link-lib=dl");
    }
}
```

Usa los cfg personalizados en codigo:

```rust
fn main() {
    // Variables de entorno inyectadas por build.rs
    let target = env!("BUILD_TARGET");
    println!("Compilado para: {target}");

    #[cfg(static_build)]
    println!("Este es un binario enlazado estaticamente");

    #[cfg(wasm)]
    println!("Ejecutando en WebAssembly");
}
```

---

## Verificacion

Despues de completar este ejercicio, deberias poder responder:

1. Que componentes forman un target triple y que significa cada parte?
2. Cual es la diferencia entre `x86_64-unknown-linux-gnu` y `x86_64-unknown-linux-musl`?
3. Como se instala un nuevo target con rustup y como se usa con `cargo build`?
4. Que hace `cross` internamente que lo diferencia de `cargo build --target`?
5. Por que los binarios musl son preferidos para contenedores Docker tipo scratch?
6. Como se configura un linker personalizado en `.cargo/config.toml`?
7. Cual es la diferencia entre `#[cfg()]` (atributo) y `cfg!()` (macro)?
8. Como se estructura un crate que soporta multiples plataformas de forma limpia?
9. Por que se prefiere `rustls` sobre `openssl-sys` al compilar con musl?
10. Como se implementa una CI matrix que compila para 5+ targets diferentes?

---

## Lo que Aprendiste

- **Target triples**: formato `arch-vendor-os-env` y como cada componente afecta la compilacion
- **rustup target**: instalar y gestionar targets de compilacion cruzada
- **Compilacion condicional**: `#[cfg()]`, `cfg!()`, `cfg_if!`, combinaciones logicas con `all`, `any`, `not`
- **Feature flags**: abstraer diferencias de plataforma detras de features de Cargo
- **Configuracion de linkers**: `.cargo/config.toml` para linkers, rustflags, y runners por target
- **cross**: cross-compilacion via Docker sin configurar toolchains manualmente
- **musl y enlazado estatico**: binarios sin dependencias dinamicas para contenedores minimos
- **WebAssembly**: compilar Rust a WASM con wasm-pack y wasm-bindgen
- **CI multi-target**: matrices de GitHub Actions para builds automatizados en multiples plataformas
- **build.rs**: deteccion de target en tiempo de compilacion y cfg flags personalizados
- **Patrones de produccion**: estructura de crates multi-plataforma con traits y modulos condicionales

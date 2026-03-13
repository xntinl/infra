# 11. no_std and Embedded

**Difficulty**: Insane

## The Challenge

Build a bare-metal sensor aggregation firmware for an ARM Cortex-M4F target (`thumbv7em-none-eabihf`) that reads simulated sensor data, applies a moving-average filter, and blinks an LED at a rate proportional to the filtered value. The firmware must run under QEMU's `lm3s6965evb` machine model for development and testing, with no operating system, no standard library, and no heap allocator. You will use the RTIC (Real-Time Interrupt-driven Concurrency) framework for task scheduling, `embedded-hal` traits for hardware abstraction, and `defmt` for structured logging over a debug probe.

This exercise forces you to confront what `std` actually provides: a heap allocator, panic infrastructure, I/O, threads, and the runtime that calls `main`. Without `std`, you must supply a panic handler, a memory layout (linker script), and — if you want dynamic allocation — an allocator. The `core` library gives you `Option`, `Result`, iterators, `core::fmt`, and atomics. The `alloc` crate adds `Vec`, `String`, `Box`, and `Arc`, but requires a global allocator. Here, you will work with `core` only — fixed-size buffers, no heap.

The deeper lesson is understanding the `embedded-hal` trait ecosystem: how `embedded-hal::digital::OutputPin`, `embedded-hal::delay::DelayNs`, and `embedded-hal::spi::SpiDevice` define a hardware-agnostic interface that PAC (Peripheral Access Crate) and HAL crate authors implement. Your firmware should depend only on `embedded-hal` traits, not on a specific HAL, making it portable across chip families.

## Acceptance Criteria

- [ ] Crate compiles with `#![no_std]` and `#![no_main]` — no dependency on `std`
- [ ] Target is `thumbv7em-none-eabihf` — builds with `cargo build --target thumbv7em-none-eabihf`
- [ ] Panic handler defined with `#[panic_handler]` — halts or logs via `defmt` and enters infinite loop
- [ ] Memory layout via `memory.x` linker script — FLASH and RAM regions defined for lm3s6965evb (256K flash, 64K RAM)
- [ ] RTIC application with at least two tasks: a periodic sensor read task and an LED toggle task
- [ ] RTIC uses hardware task priorities — sensor read at higher priority than LED toggle
- [ ] `embedded-hal` traits used for GPIO and delay — firmware logic does not import any chip-specific HAL directly
- [ ] Moving-average filter uses a fixed-size circular buffer (`[u16; N]`) — no heap allocation
- [ ] `defmt` logging compiles and produces output when run under `probe-rs` or QEMU with semihosting
- [ ] Runs under QEMU: `qemu-system-arm -machine lm3s6965evb -nographic -semihosting-config enable=on,target=native -kernel target/thumbv7em-none-eabihf/release/firmware`
- [ ] Binary size under 32KB (release, `opt-level = "s"`, LTO enabled)
- [ ] No `unsafe` outside of RTIC-generated code and the panic handler
- [ ] `cargo clippy --target thumbv7em-none-eabihf` passes with no warnings

## Background

### `core` vs `alloc` vs `std`

The Rust standard library is layered:

- **`core`**: Platform-agnostic, no OS, no allocator. Provides `Option`, `Result`, `Iterator`, `fmt`, `ops`, `slice`, `str`, atomics, `core::cell`, `core::ptr`. Available everywhere.
- **`alloc`**: Requires a global allocator (`#[global_allocator]`). Adds `Vec`, `String`, `Box`, `Rc`, `Arc`, `BTreeMap`. You can use this on embedded if you provide an allocator (e.g., `embedded-alloc`), but for this exercise you must not.
- **`std`**: Requires an OS. Adds `fs`, `net`, `io`, `thread`, `process`, `env`, `HashMap` (depends on random state). Not available on bare-metal.

### RTIC

RTIC (Real-Time Interrupt-driven Concurrency) is a framework for building embedded applications. RTIC v2 uses procedural macros to generate interrupt handlers, resource sharing with priority-based ceiling locks, and message passing between tasks. Tasks are dispatched by hardware interrupts, providing bounded worst-case latency. The framework is `#![no_std]` native.

Key files in `rtic-rs/rtic`: `macros/src/codegen/` generates the dispatching infrastructure, `rtic/src/lib.rs` provides the framework's public API. Study the `rtic-rs/defmt-app-template` for project scaffolding.

### `defmt`

`defmt` (de-format) is a logging framework designed for resource-constrained environments. Unlike `log` or `println!`, `defmt` transmits format string indices (not the strings themselves) over the wire, with the host-side tooling reconstructing the full message. This makes logging nearly zero-cost in flash and CPU time. See Ferrous Systems' blog post: "defmt, a highly efficient Rust logging framework for embedded devices."

### The PAC/HAL Stack

```
Your firmware
    |
    v
embedded-hal traits (OutputPin, DelayNs, SpiDevice)
    |
    v
HAL crate (e.g., lm3s6965-hal) — implements traits for specific chip
    |
    v
PAC (Peripheral Access Crate, e.g., lm3s6965) — auto-generated from SVD files
    |
    v
Hardware registers (MMIO)
```

## Starting Points

1. Install the target: `rustup target add thumbv7em-none-eabihf`
2. Install QEMU: `brew install qemu` (macOS) or `apt install qemu-system-arm` (Linux)
3. Install probe-rs: `cargo install probe-rs-tools` — needed for `defmt` decoding
4. Clone the template: `cargo generate --git https://github.com/rtic-rs/defmt-app-template` — study the structure, then throw it away and build your own
5. Write a minimal `#![no_std]` binary first — just a panic handler and an infinite loop — and get it to compile and run under QEMU before adding RTIC

### Cargo.toml essentials

```toml
[dependencies]
cortex-m = { version = "0.7", features = ["critical-section-single-core"] }
cortex-m-rt = "0.7"
rtic = { version = "2", features = ["thumbv7-backend"] }
defmt = "0.3"
defmt-rtt = "0.4"
panic-probe = { version = "0.3", features = ["print-defmt"] }
embedded-hal = "1.0"

[profile.release]
opt-level = "s"
lto = true
debug = true  # keep debug info for defmt
```

### `.cargo/config.toml`

```toml
[target.thumbv7em-none-eabihf]
runner = "probe-rs run --chip LM3S6965"
rustflags = ["-C", "link-arg=-Tlink.x", "-C", "link-arg=-Tdefmt.x"]

[build]
target = "thumbv7em-none-eabihf"
```

## Going Further

- Add a real SPI sensor driver behind `embedded-hal::spi::SpiDevice` and test it with `embedded-hal-mock`.
- Implement a heapless ring buffer using `heapless::Queue` for inter-task communication.
- Port the firmware to a real board (STM32F4 Discovery, nRF52840 DK) — only the HAL crate changes, not your application logic.
- Add a watchdog timer using `embedded-hal::watchdog::Watchdog` — reset if the sensor task misses a deadline.
- Measure stack usage with `flip-link` (Knurling-rs tool) and paint-based stack watermarking.
- Implement `defmt::Format` for your custom sensor data types.
- Write unit tests that run on the host (not the target) by abstracting hardware behind `embedded-hal` traits and using `embedded-hal-mock` in `#[cfg(test)]`.

## Resources

- **Book**: *The Embedded Rust Book* — [docs.rust-embedded.org/book](https://docs.rust-embedded.org/book/)
- **Book**: *The Discovery Book* (STM32F3) — [docs.rust-embedded.org/discovery](https://docs.rust-embedded.org/discovery/)
- **Framework**: RTIC v2 — [rtic.rs](https://rtic.rs/) — study `rtic-rs/rtic/examples/` on GitHub
- **Template**: `rtic-rs/defmt-app-template` — [github.com/rtic-rs/defmt-app-template](https://github.com/rtic-rs/defmt-app-template)
- **Blog**: Ferrous Systems — "defmt, a highly efficient Rust logging framework" — [ferrous-systems.com/blog/defmt](https://ferrous-systems.com/blog/defmt/)
- **Blog**: Ferrous Systems — "How we built our 2025 Embedded World Demos" — [ferrous-systems.com/blog/embedded-world-2025-demos](https://ferrous-systems.com/blog/embedded-world-2025-demos/)
- **Tool**: probe-rs — [probe.rs](https://probe.rs/) — flash, debug, and log from embedded targets
- **Tool**: `flip-link` — [github.com/knurling-rs/flip-link](https://github.com/knurling-rs/flip-link) — stack overflow protection
- **Crate**: `embedded-hal` 1.0 — [docs.rs/embedded-hal](https://docs.rs/embedded-hal/1.0.0/embedded_hal/)
- **Crate**: `heapless` — fixed-capacity collections — [docs.rs/heapless](https://docs.rs/heapless)
- **Crate**: `embedded-hal-mock` — [docs.rs/embedded-hal-mock](https://docs.rs/embedded-hal-mock)
- **Source**: `cortex-m-rt/src/lib.rs` — the runtime that calls your `#[entry]` function — [github.com/rust-embedded/cortex-m](https://github.com/rust-embedded/cortex-m)
- **Reference**: ARM Cortex-M4 Technical Reference Manual — understand NVIC priorities for RTIC task scheduling
- **QEMU**: ARM system emulator docs — [qemu.org/docs/master/system/arm/stellaris.html](https://www.qemu.org/docs/master/system/arm/stellaris.html)

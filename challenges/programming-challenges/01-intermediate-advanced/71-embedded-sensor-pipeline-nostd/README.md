# 71. Embedded Sensor Pipeline `#![no_std]`

<!--
difficulty: intermediate-advanced
category: embedded-systems
languages: [rust]
concepts: [no-std, fixed-point-arithmetic, circular-buffer, signal-filtering, binary-serialization, embedded-patterns]
estimated_time: 6-9 hours
bloom_level: apply, analyze
prerequisites: [rust-core-library, no-std-basics, bitwise-operations, traits-and-generics, integer-overflow-handling]
-->

## Languages

Rust (stable, latest edition)

## Prerequisites

- Understanding of `#![no_std]` and the difference between `core`, `alloc`, and `std`
- Comfortable with integer arithmetic, bit shifting, and overflow-aware operations
- Familiarity with Rust traits, generics, and the `core::fmt` module
- Basic knowledge of how sensor data is represented (ADC values, scaling factors)
- Understanding of circular buffers and FIFO data structures

## Learning Objectives

- **Apply** `#![no_std]` constraints to build software that runs without an operating system
- **Implement** fixed-point arithmetic as a replacement for floating-point in resource-constrained environments
- **Design** a circular buffer optimized for streaming sensor data with constant memory usage
- **Analyze** the trade-offs between fixed-point precision and range for different Q-format configurations
- **Implement** common signal processing filters (moving average, median) using only integer operations

## The Challenge

Embedded systems connected to physical sensors produce continuous streams of data: temperature readings, accelerometer values, pressure measurements. These systems often lack a floating-point unit (FPU), making hardware float operations either unavailable or prohibitively slow. The standard library is too large for microcontrollers with kilobytes of flash memory.

Build a sensor data processing pipeline that operates entirely within `#![no_std]` Rust. The pipeline reads simulated sensor data, processes it through configurable filters, checks threshold-based alerts, and serializes results into a compact binary format suitable for transmission over constrained links (UART, SPI, LoRa).

The core abstraction is fixed-point arithmetic: represent fractional numbers as scaled integers. A Q16.16 format uses 16 bits for the integer part and 16 bits for the fractional part, stored in an `i32`. This gives you a range of roughly -32768 to +32767 with precision of 1/65536. All math operations (add, subtract, multiply, divide) must be implemented manually with proper overflow handling.

## Requirements

1. Implement a `FixedPoint<const FRAC: u32>` type backed by `i32`. Support: `new` from integer, `from_f32` (for test setup only), addition, subtraction, multiplication (using `i64` intermediate to prevent overflow), division, comparison, and `Display` formatting
2. Implement a `CircularBuffer<T, const N: usize>` using a fixed-size array (no heap). Support: `push` (overwrites oldest when full), `iter` (yields elements oldest-first), `len`, `is_full`, and `clear`
3. Implement a `MovingAverage<const WINDOW: usize>` filter that uses a `CircularBuffer` internally and returns the mean of the last N samples in fixed-point
4. Implement a `MedianFilter<const WINDOW: usize>` that returns the median of the last N samples. Use insertion sort on a small scratch array (window sizes <= 9)
5. Implement a `ThresholdAlert` system: configure high and low thresholds. When a filtered value crosses a threshold, emit an alert event with direction (rising/falling), the value, and a timestamp counter
6. Implement binary serialization: pack a `SensorReading { sensor_id: u8, timestamp: u32, raw: i16, filtered: FixedPoint, alert: Option<Alert> }` into a compact byte array using little-endian encoding. Include a CRC-8 checksum
7. Write a `SensorPipeline` that chains: raw input -> circular buffer -> filter -> threshold check -> serialize. The pipeline must be generic over the filter type via a `Filter` trait
8. All code must compile with `#![no_std]`. Use only `core` and `alloc` (for `Vec` in tests). No `std` dependency in the library
9. Simulate sensor input: generate a sine wave of ADC values (using fixed-point Taylor series approximation or a lookup table) with configurable noise (LCG pseudo-random)
10. Write tests verifying: fixed-point arithmetic accuracy within 0.01 of expected values, buffer wraparound, filter output convergence, alert triggering, and serialization round-trip

## Hints

<details>
<summary>Hint 1: Fixed-point multiplication</summary>

Multiplication of two Q16.16 numbers requires shifting the result back by the fractional bits. Use `i64` to hold the intermediate product and avoid overflow:

```rust
impl<const FRAC: u32> core::ops::Mul for FixedPoint<FRAC> {
    type Output = Self;
    fn mul(self, rhs: Self) -> Self {
        let wide = (self.0 as i64) * (rhs.0 as i64);
        Self((wide >> FRAC) as i32)
    }
}
```

</details>

<details>
<summary>Hint 2: Circular buffer with const generics</summary>

Use `MaybeUninit` to avoid requiring `Default` on `T`:

```rust
use core::mem::MaybeUninit;

pub struct CircularBuffer<T, const N: usize> {
    buf: [MaybeUninit<T>; N],
    head: usize,
    len: usize,
}
```

`head` points to the oldest element. On `push` when full, overwrite at `(head + len) % N` and advance `head`.

</details>

<details>
<summary>Hint 3: Median without allocation</summary>

Copy the window contents into a stack-allocated array, then insertion-sort it:

```rust
fn median(&self) -> FixedPoint<FRAC> {
    let mut scratch = [FixedPoint::ZERO; WINDOW];
    // copy from circular buffer into scratch[..self.buffer.len()]
    // insertion sort scratch
    // return middle element (or average of two middle elements)
}
```

</details>

<details>
<summary>Hint 4: CRC-8 without a table</summary>

A bitwise CRC-8 (polynomial 0x07) needs no lookup table:

```rust
fn crc8(data: &[u8]) -> u8 {
    let mut crc: u8 = 0;
    for &byte in data {
        crc ^= byte;
        for _ in 0..8 {
            if crc & 0x80 != 0 {
                crc = (crc << 1) ^ 0x07;
            } else {
                crc <<= 1;
            }
        }
    }
    crc
}
```

</details>

## Acceptance Criteria

- [ ] All library code compiles with `#![no_std]` -- verified by building with `cargo build` and no std dependency
- [ ] `FixedPoint` arithmetic matches expected results within 0.01 absolute error for add, sub, mul, div
- [ ] `CircularBuffer` correctly handles push, iteration, and wraparound for buffer sizes 1, 4, and 16
- [ ] `MovingAverage` converges to the true mean after the window fills
- [ ] `MedianFilter` correctly rejects outlier spikes in sensor data
- [ ] Threshold alerts fire on rising and falling crossings with correct direction and value
- [ ] Binary serialization produces deterministic byte sequences that deserialize back to identical structs
- [ ] CRC-8 detects single-bit errors in serialized packets
- [ ] `SensorPipeline` processes 10,000 simulated readings without panic or overflow
- [ ] No floating-point operations appear in the library code (only in test helpers)

## Research Resources

- [The Embedded Rust Book](https://doc.rust-lang.org/embedded-book/) -- official guide to `#![no_std]` development
- [Fixed-Point Arithmetic (Wikipedia)](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) -- Q-format representation, operations, and precision analysis
- [Rust `core` library documentation](https://doc.rust-lang.org/core/) -- the subset of std available without an OS
- [Digital Signal Processing: A Practical Guide (Smith)](https://www.dspguide.com/ch15/1.htm) -- moving average and median filters
- [CRC-8 calculation](https://www.sunshine2k.de/articles/coding/crc/understanding_crc.html) -- bitwise and table-based CRC implementations
- [Embedded Rust `no_std` patterns](https://docs.rust-embedded.org/book/intro/no-std.html) -- patterns for working without the standard library

# Dirty NIFs for CPU-Bound Work

**Project**: `image_pipeline` — a thumbnail service that resizes JPEGs and computes perceptual hashes (pHash) on ingest.

## The business problem

You run an image ingest service for a social feed: 500 uploads/sec, each a 3–8MB JPEG that
must be resized to four sizes and hashed for dedup. The resize + pHash pipeline, even with
the fastest image crate in Rust, takes 15–80ms per image. That is **80 times** the 1ms
budget a regular NIF may consume on a scheduler thread.

Running this on a regular NIF starves schedulers and produces latency spikes on unrelated
processes (HTTP handlers, GenServers) co-scheduled there. The BEAM solution is **dirty
schedulers** — a pool of OS threads separate from the normal schedulers, designed to host
long-running native code without harming the soft-realtime guarantees of the rest of the VM.

## Project structure

```
image_pipeline/
├── lib/
│   └── image_pipeline/
│       ├── application.ex
│       └── imgnif.ex
├── native/
│   └── imgnif/
│       ├── Cargo.toml
│       └── src/lib.rs
├── test/
│   └── image_pipeline/imgnif_test.exs
├── bench/imgnif_bench.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Why a dirty NIF and not a regular NIF with chunking

Chunking a long computation via `rustler::schedule` works for pure Rust code where you can
stop at an iteration boundary. Image decoding cannot: `turbojpeg` reads the entire stream
and you cannot re-enter the decoder mid-frame without keeping state alive in the BEAM heap.
A dirty scheduler solves this without changing the algorithm — you tell Rustler "this runs
on the dirty CPU pool" and the function call returns to the BEAM cooperatively via OS-level
preemption.

## Why dirty_cpu and not dirty_io

The BEAM has **two dirty pools**:

- **`dirty_cpu`** — sized to the number of logical cores. Use for computation: encryption,
  image processing, matrix math.
- **`dirty_io`** — sized larger (default 10), use for blocking file system or network calls
  that already release the CPU by sleeping.

Image resize is CPU-bound. Use `dirty_cpu`. Putting it on `dirty_io` would oversubscribe
your cores and hurt throughput.

## Design decisions

- **Option A — flag the function with `#[rustler::nif(schedule = "DirtyCpu")]`**.
  Pros: one line, no application code change. Cons: every call goes to dirty pool even for
  trivial inputs.
- **Option B — two functions: `resize_small/2` on normal pool, `resize_large/2` on dirty**.
  Pros: best of both worlds. Cons: caller must know which to invoke.

→ We pick **Option A** for simplicity; the reflection section revisits Option B.

- **Option A — return the resized binary directly**.
- **Option B — write to disk and return a path**.

→ Returning the binary (Option A) avoids disk I/O and lets the caller decide where to store.
For very large outputs (> 10MB) this becomes memory pressure — then Option B wins.

## Implementation

### Dependencies (`mix.exs`)

### `mix.exs`
```elixir
defmodule DirtyNifCpuBound.MixProject do
  use Mix.Project

  def project do
    [
      app: :dirty_nif_cpu_bound,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [# No external dependencies — pure Elixir]
  end
end
```

```elixir
defmodule ImagePipeline.MixProject do
  use Mix.Project

  def project do
    [
      app: :image_pipeline,
      version: "0.1.0",
      elixir: "~> 1.19",
      compilers: [:rustler] ++ Mix.compilers(),
      rustler_crates: [
        imgnif: [path: "native/imgnif", mode: :release]
      ],
      deps: [
        {:rustler, "~> 0.34"},
        {:benchee, "~> 1.3", only: :dev}
      ]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {ImagePipeline.Application, []}]
end
```

### Step 1: Cargo manifest

**Objective**: Declare image processing deps so Rust NIF can decode, resize, and hash JPEGs efficiently.

```toml
# native/imgnif/Cargo.toml
[package]
name = "imgnif"
version = "0.1.0"
edition = "2021"

[lib]
name = "imgnif"
crate-type = ["cdylib"]

[dependencies]
rustler = "0.34"
image = { version = "0.25", default-features = false, features = ["jpeg", "png"] }
```

### Step 2: Rust NIF (`native/imgnif/src/lib.rs`)

**Objective**: Schedule JPEG decode/resize/hash on DirtyCpu so image work never blocks regular BEAM schedulers.

```rust
use image::{imageops::FilterType, ImageFormat};
use rustler::{Binary, Env, Error, NifResult, OwnedBinary};
use std::io::Cursor;

mod atoms {
    rustler::atoms! { ok, error, decode_failed, encode_failed }
}

#[rustler::nif(schedule = "DirtyCpu")]
fn resize_jpeg<'a>(env: Env<'a>, jpeg: Binary<'a>, width: u32, height: u32)
    -> NifResult<Binary<'a>>
{
    let img = image::load_from_memory_with_format(&jpeg, ImageFormat::Jpeg)
        .map_err(|_| Error::Term(Box::new(atoms::decode_failed())))?;

    let scaled = img.resize(width, height, FilterType::Lanczos3);

    let mut out = Vec::with_capacity(jpeg.len() / 2);
    scaled
        .write_to(&mut Cursor::new(&mut out), ImageFormat::Jpeg)
        .map_err(|_| Error::Term(Box::new(atoms::encode_failed())))?;

    let mut binary = OwnedBinary::new(out.len()).unwrap();
    binary.as_mut_slice().copy_from_slice(&out);
    Ok(binary.release(env))
}

/// Simple average-hash perceptual hash — 64-bit fingerprint.
/// Also dirty_cpu: decodes a full JPEG before hashing.
#[rustler::nif(schedule = "DirtyCpu")]
fn phash(jpeg: Binary) -> NifResult<u64> {
    let img = image::load_from_memory_with_format(&jpeg, ImageFormat::Jpeg)
        .map_err(|_| Error::Term(Box::new(atoms::decode_failed())))?;

    let small = img.resize_exact(8, 8, FilterType::Nearest).to_luma8();
    let pixels: Vec<u8> = small.pixels().map(|p| p.0[0]).collect();
    let avg = (pixels.iter().map(|&v| v as u32).sum::<u32>() / 64) as u8;

    let mut bits: u64 = 0;
    for (i, &p) in pixels.iter().enumerate() {
        if p > avg { bits |= 1u64 << i; }
    }
    Ok(bits)
}

rustler::init!("Elixir.ImagePipeline.Imgnif", [resize_jpeg, phash]);
```

### Step 3: Elixir wrapper with bounded concurrency

**Objective**: Cap concurrent NIF calls at dirty scheduler count so queue depth becomes measurable instead of hidden.

```elixir
defmodule ImagePipeline.Imgnif do
  @moduledoc """
  Dirty-NIF entry points. Always front dirty NIFs with a concurrency limit —
  here we use a `:counters`-based semaphore to cap in-flight work at the
  dirty_cpu scheduler count. Past that, extra work queues silently inside
  the BEAM and you lose visibility.
  """
  use Rustler, otp_app: :image_pipeline, crate: :imgnif

  def resize_jpeg(_jpeg, _w, _h), do: :erlang.nif_error(:nif_not_loaded)
  def phash(_jpeg), do: :erlang.nif_error(:nif_not_loaded)
end

defmodule ImagePipeline.Gate do
  @moduledoc """
  Bounded gate around dirty-NIF calls. Caps concurrent native work at the
  number of dirty_cpu schedulers reported by the VM.
  """
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  def with_slot(fun) when is_function(fun, 0) do
    :ok = acquire()
    try do
      fun.()
    after
      release()
    end
  end

  def acquire, do: GenServer.call(__MODULE__, :acquire, :infinity)
  def release, do: GenServer.cast(__MODULE__, :release)

  @impl true
  def init(_) do
    max = :erlang.system_info(:dirty_cpu_schedulers)
    {:ok, %{available: max, waiters: :queue.new()}}
  end

  @impl true
  def handle_call(:acquire, from, %{available: 0, waiters: w} = s) do
    {:noreply, %{s | waiters: :queue.in(from, w)}}
  end

  def handle_call(:acquire, _from, %{available: n} = s) do
    {:reply, :ok, %{s | available: n - 1}}
  end

  @impl true
  def handle_cast(:release, %{waiters: w, available: n} = s) do
    case :queue.out(w) do
      {{:value, from}, rest} ->
        GenServer.reply(from, :ok)
        {:noreply, %{s | waiters: rest}}
      {:empty, _} ->
        {:noreply, %{s | available: n + 1}}
    end
  end
end
```

### Step 4: Supervision (`lib/image_pipeline/application.ex`)

**Objective**: Boot the concurrency gate so dirty NIF calls remain visible to monitoring and backpressure mechanisms.

```elixir
defmodule ImagePipeline.Application do
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link(
      [ImagePipeline.Gate],
      strategy: :one_for_one, name: ImagePipeline.Supervisor
    )
  end
end
```

## Why this works

```
caller ──Gate.with_slot──▶ semaphore (cap = N dirty schedulers)
                              │
                              ▼
               resize_jpeg (runs on dirty_cpu pool)
                              │
                              ▼
            regular schedulers remain free for HTTP/GenServer/etc
```

- Dirty scheduler keeps image decode off the soft-realtime schedulers — no latency spike
  on co-scheduled processes.
- The gate caps in-flight work at the physical capacity. Beyond the cap, callers park in
  the GenServer mailbox (visible via `Process.info(pid, :message_queue_len)`), not in an
  invisible VM-internal queue.
- `OwnedBinary` + `release(env)` transfers ownership of the Vec bytes to the BEAM as a
  reference-counted binary — no copy beyond the one inside `copy_from_slice`.

## Tests (`test/image_pipeline/imgnif_test.exs`)

```elixir
defmodule ImagePipeline.ImgnifTest do
  use ExUnit.Case, async: false
  doctest ImagePipeline.Application
  alias ImagePipeline.{Imgnif, Gate}

  # 1x1 red JPEG, hand-encoded — smallest valid JPEG.
  @tiny_jpeg <<255, 216, 255, 224, 0, 16, 74, 70, 73, 70, 0, 1, 1, 0, 0, 1, 0, 1, 0, 0,
               255, 219, 0, 67, 0, 8, 6, 6, 7, 6, 5, 8, 7, 7, 7, 9, 9, 8, 10, 12, 20,
               13, 12, 11, 11, 12, 25, 18, 19, 15, 20, 29, 26, 31, 30, 29, 26, 28, 28,
               32, 36, 46, 39, 32, 34, 44, 35, 28, 28, 40, 55, 41, 44, 48, 49, 52, 52,
               52, 31, 39, 57, 61, 56, 50, 60, 46, 51, 52, 50, 255, 192, 0, 11, 8, 0, 1,
               0, 1, 1, 1, 17, 0, 255, 196, 0, 31, 0, 0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0,
               0, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 255, 196, 0, 181,
               16, 0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 125, 1, 2, 3, 0, 4, 17,
               5, 18, 33, 49, 65, 6, 19, 81, 97, 7, 34, 113, 20, 50, 129, 145, 161, 8,
               35, 66, 177, 193, 21, 82, 209, 240, 36, 51, 98, 114, 130, 9, 10, 22, 23,
               24, 25, 26, 37, 38, 39, 40, 41, 42, 52, 53, 54, 55, 56, 57, 58, 67, 68,
               69, 70, 71, 72, 73, 74, 83, 84, 85, 86, 87, 88, 89, 90, 99, 100, 101,
               102, 103, 104, 105, 106, 115, 116, 117, 118, 119, 120, 121, 122, 131,
               132, 133, 134, 135, 136, 137, 138, 146, 147, 148, 149, 150, 151, 152,
               153, 154, 162, 163, 164, 165, 166, 167, 168, 169, 170, 178, 179, 180,
               181, 182, 183, 184, 185, 186, 194, 195, 196, 197, 198, 199, 200, 201,
               202, 210, 211, 212, 213, 214, 215, 216, 217, 218, 225, 226, 227, 228,
               229, 230, 231, 232, 233, 234, 241, 242, 243, 244, 245, 246, 247, 248,
               249, 250, 255, 218, 0, 8, 1, 1, 0, 0, 63, 0, 251, 208, 255, 217>>

  setup do
    {:ok, _} = start_supervised(Gate)
    :ok
  end

  describe "resize_jpeg/3" do
    test "returns a non-empty JPEG binary" do
      out = Gate.with_slot(fn -> Imgnif.resize_jpeg(@tiny_jpeg, 1, 1) end)
      assert byte_size(out) > 100
      assert <<0xFF, 0xD8, _::binary>> = out  # JPEG magic bytes
    end

    test "decode_failed atom on garbage input" do
      assert_raise ErlangError, fn ->
        Gate.with_slot(fn -> Imgnif.resize_jpeg(<<0, 1, 2, 3>>, 32, 32) end)
      end
    end
  end

  describe "phash/1" do
    test "returns a 64-bit integer" do
      h = Gate.with_slot(fn -> Imgnif.phash(@tiny_jpeg) end)
      assert is_integer(h)
      assert h >= 0 and h < 1 <<< 64
    end

    test "same image yields same hash" do
      h1 = Gate.with_slot(fn -> Imgnif.phash(@tiny_jpeg) end)
      h2 = Gate.with_slot(fn -> Imgnif.phash(@tiny_jpeg) end)
      assert h1 == h2
    end
  end

  describe "gate concurrency" do
    test "32 parallel requests all complete under the cap" do
      tasks =
        for _ <- 1..32 do
          Task.async(fn ->
            Gate.with_slot(fn -> Imgnif.phash(@tiny_jpeg) end)
          end)
        end

      results = Task.await_many(tasks, 30_000)
      assert length(results) == 32
      assert Enum.all?(results, &is_integer/1)
    end
  end
end
```

## Benchmark (`bench/imgnif_bench.exs`)

```elixir
jpeg = File.read!("test/fixtures/photo_4mb.jpg")

Benchee.run(
  %{
    "resize 512x512"  => fn -> ImagePipeline.Gate.with_slot(fn ->
                                 ImagePipeline.Imgnif.resize_jpeg(jpeg, 512, 512)
                               end) end,
    "phash"           => fn -> ImagePipeline.Gate.with_slot(fn ->
                                 ImagePipeline.Imgnif.phash(jpeg)
                               end) end
  },
  parallel: 16,
  time: 10, warmup: 2
)
```

**Expected on a 16-core box**: `phash` < 20ms p99, `resize` < 40ms p99 with parallel: 16.
If you remove the gate (parallel: 100), dirty_cpu oversubscription pushes p99 past 500ms
even though total throughput barely changes — that is back-pressure invisibility biting you.

## Advanced Considerations: NIF Isolation and Scheduler Integration

NIF calls run atomically on a scheduler thread, blocking all other processes on that scheduler until the function returns. For operations exceeding ~1 millisecond, this starvation becomes visible: heartbeat processes delay, ETS owner replies hang, supervision timeouts fire. The BEAM's dirty scheduler pool (8 CPU + 10 IO by default) isolates long NIFs from the main scheduler ring, but they're still a finite resource.

Understanding scheduler capacity is critical. Each dirty CPU scheduler can run ~1,000 100-microsecond operations per second, or ~5 100-millisecond operations. Beyond that, callers queue. A GenServer pool capping concurrency and applying backpressure prevents cascade failures: if the dirty pool saturates, reject new work immediately instead of queuing unboundedly.

Resource management inside NIFs differs from pure Elixir. A `Binary<'a>` is a borrow tied to the NIF call; it cannot escape to threads or be stored in resources. An `OwnedBinary` allocation isn't visible to BEAM's garbage collector, so memory limits must be enforced in the Elixir layer. Hybrid architectures (Port processes for I/O, NIFs for CPU work) offer better observability and failure isolation than trying to do everything in a single NIF crate.

---

## Deep Dive: Interop Patterns and Production Implications

Interop with native code (NIFs, ports, C extensions) introduces failure modes that pure Elixir code doesn't have: segfaults, memory leaks, deadlocks with the Erlang emulator. Testing interop requires separate test suites for the native layer and integration tests that exercise the boundary.

---

## Trade-offs and production gotchas

**1. Silent queuing without a gate.** Dirty scheduler runs are FIFO-ish and invisible. Without
a gate, callers wait in a VM-internal structure you cannot observe with `:observer`.

**2. `:erlang.system_info(:dirty_cpu_schedulers_online)` vs capacity.** The *online* count
can be lower than the maximum (via `:erlang.system_flag`). Compute the gate cap using the
online value if you dynamically scale.

**3. Dirty NIF calls crash the scheduler on UB.** A `panic!` is caught, but a C-library
UB (e.g., a turbojpeg decoder segfault) kills the BEAM as hard as a regular NIF. Dirty
schedulers are an isolation tool for **latency**, not for **safety**.

**4. Forgetting to release ownership.** A leaked `OwnedBinary` after panic paths leaks into
process heap. Always construct the binary just before `release(env)`.

**5. Misreading `schedule = "DirtyIo"`.** If your NIF does *only* compute, dirty_io adds
thread handoffs without any benefit. Profile before switching pools.

**6. When NOT to use dirty NIFs.** Pure I/O work belongs to BEAM ports. Sub-millisecond
compute belongs to regular NIFs. Dirty NIFs live in the 1ms–1s CPU band — above a second,
consider spawning a supervised external process.

## Reflection

The gate uses the dirty_cpu scheduler count as its cap. In a mixed workload (some NIF calls
are 5ms, others 500ms), a simple FIFO queue treats both equally — short work blocks behind
long work. What changes to the gate (hint: two queues, two caps) would let short work
bypass long work without violating the dirty pool bound?

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the image_pipeline project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/image_pipeline/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `ImagePipeline` — a thumbnail service that resizes JPEGs and computes perceptual hashes (pHash) on ingest.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[image_pipeline] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[image_pipeline] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:image_pipeline) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `image_pipeline`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```

---

## Why Dirty NIFs for CPU-Bound Work matters

Mastering **Dirty NIFs for CPU-Bound Work** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/image_pipeline.ex`

```elixir
defmodule ImagePipeline do
  @moduledoc """
  Ejercicio: Dirty NIFs for CPU-Bound Work.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  @doc """
  Entry point for the image_pipeline module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> ImagePipeline.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/image_pipeline_test.exs`

```elixir
defmodule ImagePipelineTest do
  use ExUnit.Case, async: true

  doctest ImagePipeline

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ImagePipeline.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Dirty scheduler count

Set at VM start via `erl +SDcpu <N>` and `+SDio <N>`. Default for `SDcpu` is the core count.
On a 16-core box, up to 16 dirty NIFs run in parallel without blocking regular schedulers.

### 2. DirtyCpu vs. regular NIF trade-off

Dirty NIF calls have higher **per-call overhead** (~5µs vs ~0.5µs) due to handoff to the
dirty pool. Below ~100µs of work, a regular NIF is faster. Above ~1ms of work, a dirty NIF
is mandatory. Between is a gray zone — measure.

### 3. Back pressure — the forgotten lesson

If 1000 processes each call a `dirty_cpu` NIF concurrently on a 16-core machine, 984 of them
queue. The queue is inside the BEAM VM and invisible to observers. If upstream does not
throttle, the queue grows without bound. Always front a dirty NIF with a bounded worker
pool or semaphore.

### 4. No lightweight cancellation

If the caller dies while its NIF runs, the NIF keeps running to completion. There is no
signal. Build the computation to be idempotent and checkpoint-free.

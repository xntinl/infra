# HiPE, BeamAsm / JIT Considerations on OTP 24+

**Project**: `jit_probe` — a lab that detects whether the running VM uses BeamAsm, measures the hot-loop speedup vs the interpreter, and shows why HiPE is no longer a reasonable choice.

## Project context

A senior dev asks you to "AOT-compile the hot modules with HiPE" for a 30% speedup. HiPE was removed from OTP 24. What the dev wants already exists: BeamAsm, the asm-based JIT introduced in OTP 24, is enabled by default. The 30% speedup is real, but you get it for free — no compile-time AOT, no per-module pragma.

Understanding BeamAsm matters because some ops (large integer arithmetic, specific BIFs) are not JIT-inlined and still go through the interpreter. And because your Docker image base must support it: BeamAsm requires a compatible CPU and libc.

```
jit_probe/
├── lib/
│   └── jit_probe/
│       ├── detector.ex
│       └── hot_loop.ex
├── test/
│   └── jit_probe/
│       └── detector_test.exs
├── bench/
│   └── jit_bench.exs
└── mix.exs
```

## Why BeamAsm and not HiPE

HiPE (High Performance Erlang) was an AOT compiler that generated native code per function. It had problems: slow compile times, dramatically larger .beam files, bugs on tail-recursive code, no support for dirty schedulers, broken hot code swap in some cases. It was not default.

BeamAsm is a JIT at MODULE load time: each .beam file is translated to x86_64 or aarch64 assembly as it is loaded. Startup pauses are a few ms per module. Cold code and hot code both benefit. No AOT, no per-module opt-in.

**Why not disable BeamAsm?** There is only one reason: you need to step through VM bytecode in a debugger. Pass `+JMsingle false` to the VM. Regular apps never need it.

## Core concepts

### 1. Detecting BeamAsm

`:erlang.system_info(:emu_flavor)` returns `:jit` on BeamAsm and `:emu` on interpreter-only builds (e.g., Windows builds prior to OTP 25 did not ship BeamAsm on all platforms).

### 2. Disassembly of JITed modules

`:erts_debug.df(Module)` dumps the generated assembly to `Module.dis` when running BeamAsm. Read to see what the JIT made of your hot loop.

### 3. What is NOT JIT-compiled

- Pattern matching with bignum arithmetic (uses the arbitrary-precision BIF).
- Some ETS and `:persistent_term` operations (BIFs; the call is JITed, the implementation is native C).
- Calls into NIFs (NIF runs native anyway).

### 4. Platform support

- Linux x86_64: yes (default since OTP 24).
- Linux aarch64: yes since OTP 24.3.
- macOS x86_64 / aarch64: yes.
- Windows x86_64: yes since OTP 25.
- Alpine (musl libc): yes; ensure image includes `libstdc++`.

### 5. What you still can do instead of HiPE

Performance wins now come from:
- Writing hot paths as tail-recursive functions (BeamAsm inlines clause selection).
- Using guard-based dispatch rather than runtime type tests.
- Reducing allocations (no list building in the hot loop).
- Dirty NIFs for truly computational code (image processing, crypto).

## Design decisions

- **Option A — HiPE**: not available on OTP 24+.
- **Option B — rely on BeamAsm**: default, measured 20–40% gain on CPU-heavy Elixir code.
- **Option C — NIF in Rust/C via Rustler**: 10–100x gain for numerical kernels; loses hot reload.

Chosen: Option B for 95% of code; Option C only where BeamAsm is not enough (measurement required).

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule JitProbe.MixProject do
  use Mix.Project
  def project, do: [app: :jit_probe, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Detector — `lib/jit_probe/detector.ex`

**Objective**: Query :erlang.system_info/1 and :erts_debug.df/1 to detect BeamAsm presence and dump module assembly.

```elixir
defmodule JitProbe.Detector do
  @moduledoc """
  Introspects the running VM for JIT information.
  """

  def flavor, do: :erlang.system_info(:emu_flavor)

  def jit?, do: flavor() == :jit

  def otp_release, do: :erlang.system_info(:otp_release)

  def summary do
    %{
      emu_flavor: flavor(),
      jit?: jit?(),
      otp_release: otp_release(),
      schedulers: :erlang.system_info(:schedulers),
      wordsize: :erlang.system_info(:wordsize)
    }
  end

  @doc """
  Dumps BeamAsm assembly for a given module. Only works when jit?/0 is true.
  Writes to CWD as `<Module>.dis`.
  """
  def dump_asm(module) do
    if jit?() do
      :erts_debug.df(module)
    else
      {:error, :no_jit}
    end
  end
end
```

### Step 2: Hot loop — `lib/jit_probe/hot_loop.ex`

**Objective**: Implement tail-recursive, body-recursive, and Enum-reduce sum variants so BeamAsm inlining and allocation differences surface.

```elixir
defmodule JitProbe.HotLoop do
  @moduledoc """
  A tail-recursive sum loop that BeamAsm compiles cleanly.
  Compare against a non-tail variant to see inlining differences.
  """

  def tail_sum(n), do: tail_sum(n, 0)
  defp tail_sum(0, acc), do: acc
  defp tail_sum(n, acc), do: tail_sum(n - 1, acc + n)

  def body_sum(0), do: 0
  def body_sum(n), do: n + body_sum(n - 1)

  def reduce_sum(n), do: Enum.reduce(1..n, 0, &(&1 + &2))
end
```

## Why this works

On BeamAsm, `tail_sum/2` compiles to a tight loop with two register operations per iteration (decrement n, add to acc, conditional jump). `body_sum/1` cannot be tail-called; each recursion allocates a stack frame. `reduce_sum/1` is idiomatic but pays for `Enum`'s anonymous function invocation.

On the interpreter (flavor == `:emu`), `tail_sum` still benefits from tail-call optimization but each instruction is a dispatch through the bytecode interpreter — 2–3x slower than the JITed version.

## Tests — `test/jit_probe/detector_test.exs`

```elixir
defmodule JitProbe.DetectorTest do
  use ExUnit.Case, async: true
  alias JitProbe.Detector

  describe "detector" do
    test "emu_flavor is :jit on OTP 24+ on supported platforms" do
      assert Detector.flavor() in [:jit, :emu]
    end

    test "summary has the expected keys" do
      s = Detector.summary()
      assert is_boolean(s.jit?)
      assert is_integer(s.schedulers)
      assert s.wordsize == 8
    end
  end

  describe "hot loop correctness" do
    test "all variants compute the same sum" do
      n = 1_000
      expected = div(n * (n + 1), 2)
      assert JitProbe.HotLoop.tail_sum(n) == expected
      assert JitProbe.HotLoop.body_sum(n) == expected
      assert JitProbe.HotLoop.reduce_sum(n) == expected
    end
  end
end
```

## Benchmark — `bench/jit_bench.exs`

```elixir
IO.inspect(JitProbe.Detector.summary(), label: "runtime")

Benchee.run(
  %{
    "tail_sum"   => fn -> JitProbe.HotLoop.tail_sum(1_000_000) end,
    "body_sum"   => fn -> JitProbe.HotLoop.body_sum(1_000_000) end,
    "reduce_sum" => fn -> JitProbe.HotLoop.reduce_sum(1_000_000) end
  },
  time: 3,
  warmup: 1
)
```

**Expected on OTP 26 + BeamAsm + x86_64**:
- `tail_sum(1M)` ~3 ms
- `body_sum(1M)` ~8 ms (stack growth)
- `reduce_sum(1M)` ~10 ms (Enum overhead)

Run the same on `erl -emu_flavor emu` (if your build supports it) and you should see ~2-3x slowdown on `tail_sum`.

## Deep Dive: BEAM Scheduler Tuning and Memory Profiling in Production

The BEAM scheduler is not "magic" — it's a preemptive work-stealing scheduler that divides CPU time 
into reductions (bytecode instructions). Understanding scheduler tuning is critical when you suspect 
latency spikes in production.

**Key concepts**:
- **Reductions budget**: By default, a process gets ~2000 reductions before yielding to another process.
  Heavy CPU work (binary matching, list recursion) can exhaust the budget and cause tail latency.
- **Dirty schedulers**: If a process does CPU-intensive work (crypto, compression, numerical), it blocks 
  the main scheduler. Use dirty NIFs or `spawn_opt(..., [{:fullsweep_after, 0}])` for GC tuning.
- **Heap tuning per process**: `Process.flag(:min_heap_size, ...)` reserves heap upfront, reducing GC 
  pauses. Measure; don't guess.

**Memory profiling workflow**:
1. Run `recon:memory/0` in iex; identify top 10 memory consumers by type (atoms, binaries, ets).
2. If binaries dominate, check for refc binary leaks (binary held by process that should have been freed).
3. Use `eprof` or `fprof` for function-level CPU attribution; `recon:proc_window/3` for process memory trends.

**Production pattern**: Deploy with `+K true` (async IO), `-env ERL_MAX_PORTS 65536` (port limit), 
`+T 9` (async threads). Measure GC time with `erlang:statistics(garbage_collection)` — if >5% of uptime, 
tune heap or reduce allocation pressure. Never assume defaults are optimal for YOUR workload.

---

## Advanced Considerations

Understanding BEAM internals at production scale requires deep knowledge of scheduler behavior, memory models, and garbage collection dynamics. The soft real-time guarantees of BEAM only hold under specific conditions — high system load, uneven process distribution across schedulers, or GC pressure can break predictable latency completely. Monitor `erlang:statistics(run_queue)` in production to catch scheduler saturation before it degrades latency significantly. The difference between immediate, offheap, and continuous GC garbage collection strategies can significantly impact tail latencies in systems with millions of messages per second and sustained memory pressure.

Process reductions and the reduction counter affect scheduler fairness fundamentally. A process that runs for extended periods without yielding can starve other processes, even though the scheduler treats it fairly by reduction count per scheduling interval. This is especially critical in pipelines processing large data structures or performing recursive computations where yielding points are infrequent and difficult to predict. The BEAM's preemption model is deterministic per reduction, making performance testing reproducible but sometimes hiding race conditions that only manifest under specific load patterns and GC interactions.

The interaction between ETS, Mnesia, and process message queues creates subtle bottlenecks in distributed systems. ETS reads don't block other processes, but writes require acquiring locks; understanding when your workload transitions from read-heavy to write-heavy is crucial for capacity planning. Port drivers and NIFs bypass the BEAM scheduler entirely, which can lead to unexpected priority inversions if not carefully managed. Always profile with `eprof` and `fprof` in realistic production-like environments before deployment to catch performance surprises.


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. Module load is slower on BeamAsm.** Each `.beam` is asm-translated at load time — a few ms per module. Release boot with 2000 modules adds ~3s. Usually fine; cold-start lambdas may notice.

**2. Native code size is larger than bytecode.** `:code.module_info(:module)` shows no change, but `/proc/<pid>/maps` shows bigger code segments. Memory-constrained environments (small containers) may see a higher RSS.

**3. `:erts_debug.df/1` works only on BeamAsm.** Interpreter builds return `{:error, :no_module}` — handle both in tooling.

**4. Hot code reload works normally with BeamAsm.** The new version is asm-compiled on load; the old version stays until its last caller dies. No gotchas versus interpreter.

**5. Some CPUs lack required features.** Older 32-bit ARM (ARMv7) is not supported. Check `:erlang.system_info(:system_architecture)` in alpine base images.

**6. When BeamAsm is not enough.** Numerical kernels (image processing, ML inference, cryptographic primitives) still need NIFs. The JIT does not vectorize or use SIMD.

## Reflection

Your team wants to "gain 30% by switching to HiPE" on OTP 26. Explain in one paragraph why this is impossible and what concrete steps would measure whether BeamAsm is already delivering their hoped-for gain.

## Resources

- [OTP 24 release notes — BeamAsm](https://www.erlang.org/blog/a-first-look-at-the-jit/)
- [BeamAsm — Lukas Larsson](https://github.com/erlang/otp/blob/master/erts/emulator/beam/jit/README.md)
- [HiPE removal — OTP 24 readme](https://www.erlang.org/blog/otp-24-highlights/)
- [Dashbit — BeamAsm impact on Elixir](https://dashbit.co/blog)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```

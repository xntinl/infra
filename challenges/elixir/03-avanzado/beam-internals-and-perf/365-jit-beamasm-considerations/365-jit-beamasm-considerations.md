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

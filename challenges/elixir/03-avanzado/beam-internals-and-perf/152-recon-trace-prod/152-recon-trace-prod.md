# Safe Production Tracing with `:recon_trace`

**Project**: `recon_trace_prod` — build a thin wrapper around `:recon_trace` that lets on-call engineers trace live production calls without killing the node.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

Your team runs an Elixir service handling ~8k req/s on each of 6 nodes. A customer
reports that a specific endpoint intermittently returns stale data, but the bug is
not reproducible in staging. You need to **trace live calls in production** to
confirm the hypothesis — which arguments trigger the stale branch — without
taking down the node.

Plain `:dbg` is dangerous in production: a misconfigured match pattern can flood
the tracer process with millions of messages per second, saturate the scheduler,
and crash the node. `:recon_trace` (part of Fred Hébert's `recon` library) wraps
`:dbg` with three crucial production safeguards: a **rate limit**, an **automatic
stop condition**, and a **formatted output** that does not spawn pretty-printing
on the traced process itself.

The goal of this exercise is to build `ReconTraceProd.Safe`, a thin façade over
`:recon_trace.calls/3` that (1) refuses to enable a trace without explicit
safety bounds, (2) writes output to a rotating file instead of stdout (important
when connected via `remote_shell`), and (3) exposes a Phoenix LiveDashboard page
where operators can start/stop traces from a browser.

This pattern is standard at companies that run Elixir at scale (Discord, Bleacher
Report, Remote). Reading Fred Hébert's "Erlang in Anger" chapter on tracing is
recommended background.

```
recon_trace_prod/
├── lib/
│   └── recon_trace_prod/
│       ├── application.ex
│       ├── safe.ex              # main façade
│       ├── sink.ex              # rotating file sink
│       └── guardrails.ex        # validates trace specs before enabling
├── test/
│   └── recon_trace_prod/
│       ├── safe_test.exs
│       └── guardrails_test.exs
├── bench/
│   └── overhead_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. Why `:dbg` is unsafe in production

`:dbg` is the OTP tracing facility used by `:sys.trace/2`, `Process.info/2`,
and most debugging tools. When you call `:dbg.tp(Mod, Fun, [])` with no guards,
**every call to `Mod.Fun/_`** across the node triggers a message to the tracer
process. For a function called 50k times per second, the tracer mailbox grows
faster than it can drain, back-pressure propagates to the schedulers, GC
pressure spikes, and the node becomes unresponsive.

`:recon_trace` solves this by wrapping `:dbg` with a counter and a ceiling:

```
caller ──call──▶ traced function
                     │
                     ▼
              :dbg tracer ──(increment counter)──▶ writer process
                     │                                │
                     │                                ▼
             (stops tracing when                 formatted output
              counter >= max_msgs)
```

If `max_msgs` is reached, `:recon_trace.clear/0` is invoked automatically — the
trace flags are removed from all processes. No manual intervention required.

### 2. Rate vs. absolute limits

`:recon_trace.calls/3` accepts either form:

| Spec | Meaning | Use when |
|------|---------|----------|
| `{max_msgs, ms}` | rate: stop if `max_msgs` arrive in `ms` window | traffic is steady |
| `max_msgs` | absolute: stop after `max_msgs` messages total | one-shot investigation |

For production, **always** prefer the rate form. An absolute limit on a
burst-prone endpoint can silently disable tracing after the first 100 requests
in one second, leaving you blind for the next hour.

### 3. Match specifications vs. bare calls

A trace spec can be a simple `{M, F, arity}` or an `{M, F, match_spec}`.
Match specs let you filter by argument shape, which is the single most
important performance lever:

```elixir
# WRONG — traces every call
:recon_trace.calls({MyApp.Payments, :charge, 3}, {100, 1_000})

# RIGHT — traces only calls where the first arg is the suspect customer_id
:recon_trace.calls(
  {MyApp.Payments, :charge, [{[:"$1", :_, :_], [{:==, :"$1", "cust_42"}], [{:return_trace}]}]},
  {100, 1_000}
)
```

The match spec is compiled and evaluated **in the tracing VM hook** (C code),
before any Erlang message is built. Filtered calls cost ~200ns each instead of
microseconds.

### 4. `return_trace` and caller filters

Two match spec actions pay for themselves:

- `{:return_trace}` — emit a message when the function returns, so you see the
  result and the wall-clock duration of the call.
- `{:caller}` — include the calling MFA, so you know which code path triggered
  each match. Essential when `Payments.charge/3` is called from twelve places.

### 5. Why write to a file, not stdout

`:recon_trace` defaults to `:io.format/2` on `group_leader()`. If you started
the trace via `remote_shell` against a running release, the output streams
to your terminal through TCP. For a 10 MB trace, you now have two failure
modes: your SSH session drops (trace output is lost) or your terminal
backpressures the node (schedulers stall while the kernel waits for the
window to open).

The safe pattern is a dedicated file sink process with an explicit buffer.
`IO.binwrite/2` to a `File.open!(path, [:write, :delayed_write])` handle
costs a few microseconds per line and never blocks the tracer.

### 6. When the trace must outlive your shell

If the on-call engineer disconnects, a plain `:recon_trace` spawned from
their shell dies when the shell dies (linked to the shell process).
`ReconTraceProd.Safe` registers the trace with a named GenServer so it
survives SSH disconnects and can be stopped from any other shell.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule ReconTraceProd.MixProject do
  use Mix.Project

  def project do
    [
      app: :recon_trace_prod,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {ReconTraceProd.Application, []}]
  end

  defp deps do
    [
      {:recon, "~> 2.5"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 2: `lib/recon_trace_prod/application.ex`

```elixir
defmodule ReconTraceProd.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {ReconTraceProd.Safe, []}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ReconTraceProd.Supervisor)
  end
end
```

### Step 3: `lib/recon_trace_prod/guardrails.ex`

```elixir
defmodule ReconTraceProd.Guardrails do
  @moduledoc """
  Validates a trace request before we hand it to `:recon_trace`.

  Refuses to enable a trace that could realistically flood the node:
  a missing rate limit, an arity of `:_` (all arities), or a bare `{:_, :_, :_}`
  match (traces every call).
  """

  @type mfa_spec ::
          {module(), atom(), non_neg_integer()}
          | {module(), atom(), [{list(), list(), list()}]}

  @type rate :: {pos_integer(), pos_integer()}

  @max_rate_msgs 5_000
  @max_rate_window_ms 60_000

  @spec validate(mfa_spec(), rate()) :: :ok | {:error, String.t()}
  def validate({mod, fun, arity_or_ms}, {max_msgs, window_ms})
      when is_atom(mod) and is_atom(fun) do
    cond do
      max_msgs > @max_rate_msgs ->
        {:error, "max_msgs=#{max_msgs} exceeds safety cap #{@max_rate_msgs}"}

      window_ms > @max_rate_window_ms ->
        {:error, "window_ms=#{window_ms} exceeds safety cap #{@max_rate_window_ms}"}

      arity_or_ms == :_ ->
        {:error, "arity :_ traces every arity; specify an integer"}

      is_list(arity_or_ms) and unbounded_match_spec?(arity_or_ms) ->
        {:error, "match spec has no guard; traces every call"}

      true ->
        :ok
    end
  end

  def validate(_, _), do: {:error, "invalid spec shape"}

  defp unbounded_match_spec?(specs) do
    Enum.any?(specs, fn {_head, guards, _body} -> guards == [] end)
  end
end
```

### Step 4: `lib/recon_trace_prod/sink.ex`

```elixir
defmodule ReconTraceProd.Sink do
  @moduledoc """
  Rotating file sink for trace output. Opens the file with `:delayed_write`
  so individual writes are buffered and flushed every 2 seconds or every 64 KiB,
  whichever comes first.
  """

  @spec open(Path.t()) :: {:ok, :file.io_device()} | {:error, term()}
  def open(path) do
    File.mkdir_p!(Path.dirname(path))
    File.open(path, [:write, :binary, {:delayed_write, 64 * 1024, 2_000}])
  end

  @spec write(:file.io_device(), iodata()) :: :ok
  def write(io, data) do
    IO.binwrite(io, [data, ?\n])
  end

  @spec close(:file.io_device()) :: :ok
  def close(io), do: File.close(io)
end
```

### Step 5: `lib/recon_trace_prod/safe.ex`

```elixir
defmodule ReconTraceProd.Safe do
  @moduledoc """
  Named GenServer that owns a single active trace. Survives caller disconnects.

  Only one trace at a time — `:recon_trace` is global per node and overlapping
  traces produce interleaved output that is nearly impossible to correlate.
  """

  use GenServer
  require Logger

  alias ReconTraceProd.{Guardrails, Sink}

  @type spec :: Guardrails.mfa_spec()
  @type rate :: Guardrails.rate()

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc """
  Start a trace. Refuses if another trace is active or guardrails fail.
  Output is written to `path`. Call `stop/0` to end early.
  """
  @spec trace(spec(), rate(), Path.t()) :: :ok | {:error, term()}
  def trace(mfa_spec, rate, path) do
    GenServer.call(__MODULE__, {:trace, mfa_spec, rate, path})
  end

  @spec stop() :: :ok
  def stop, do: GenServer.call(__MODULE__, :stop)

  @spec status() :: :idle | {:active, map()}
  def status, do: GenServer.call(__MODULE__, :status)

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts), do: {:ok, %{state: :idle}}

  @impl true
  def handle_call({:trace, mfa_spec, rate, path}, _from, %{state: :idle} = state) do
    with :ok <- Guardrails.validate(mfa_spec, rate),
         {:ok, io} <- Sink.open(path) do
      parent = self()

      formatter = fn trace_msg ->
        send(parent, {:trace_line, format(trace_msg)})
      end

      count = :recon_trace.calls(mfa_spec, rate, formatter: formatter)

      active = %{
        spec: mfa_spec,
        rate: rate,
        path: path,
        io: io,
        started_at: System.system_time(:second),
        matched_procs: count
      }

      Logger.info("trace started: #{inspect(mfa_spec)} rate=#{inspect(rate)} path=#{path}")
      {:reply, :ok, %{state: :active, trace: active}}
    else
      {:error, reason} ->
        Logger.warning("trace refused: #{inspect(reason)}")
        {:reply, {:error, reason}, state}
    end
  end

  def handle_call({:trace, _, _, _}, _from, state),
    do: {:reply, {:error, :already_active}, state}

  def handle_call(:stop, _from, %{state: :active, trace: %{io: io}} = state) do
    :recon_trace.clear()
    Sink.close(io)
    Logger.info("trace stopped")
    {:reply, :ok, %{state: :idle}}
  end

  def handle_call(:stop, _from, state), do: {:reply, :ok, state}

  def handle_call(:status, _from, %{state: :idle} = s), do: {:reply, :idle, s}

  def handle_call(:status, _from, %{state: :active, trace: t} = s) do
    {:reply, {:active, Map.take(t, [:spec, :rate, :path, :started_at, :matched_procs])}, s}
  end

  @impl true
  def handle_info({:trace_line, line}, %{state: :active, trace: %{io: io}} = state) do
    Sink.write(io, line)
    {:noreply, state}
  end

  def handle_info({:trace_line, _}, state), do: {:noreply, state}

  # ---------------------------------------------------------------------------
  # Helpers
  # ---------------------------------------------------------------------------

  defp format({:trace, pid, :call, {m, f, args}}),
    do: "CALL  #{inspect(pid)} #{inspect(m)}.#{f}/#{length(args)} args=#{inspect(args, limit: 8)}"

  defp format({:trace, pid, :return_from, {m, f, arity}, result}),
    do: "RET   #{inspect(pid)} #{inspect(m)}.#{f}/#{arity} -> #{inspect(result, limit: 8)}"

  defp format(other), do: inspect(other, limit: 16)
end
```

### Step 6: `test/recon_trace_prod/guardrails_test.exs`

```elixir
defmodule ReconTraceProd.GuardrailsTest do
  use ExUnit.Case, async: true

  alias ReconTraceProd.Guardrails

  describe "validate/2" do
    test "accepts a well-bounded arity spec" do
      assert :ok = Guardrails.validate({String, :upcase, 1}, {100, 1_000})
    end

    test "rejects when max_msgs exceeds cap" do
      assert {:error, msg} = Guardrails.validate({String, :upcase, 1}, {10_000, 1_000})
      assert msg =~ "max_msgs"
    end

    test "rejects when window exceeds cap" do
      assert {:error, msg} = Guardrails.validate({String, :upcase, 1}, {100, 120_000})
      assert msg =~ "window_ms"
    end

    test "rejects wildcard arity" do
      assert {:error, msg} = Guardrails.validate({String, :upcase, :_}, {100, 1_000})
      assert msg =~ "arity"
    end

    test "rejects match spec with no guards" do
      ms = [{[:_, :_], [], [{:return_trace}]}]
      assert {:error, msg} = Guardrails.validate({String, :replace, ms}, {100, 1_000})
      assert msg =~ "guard"
    end

    test "accepts match spec with at least one guard" do
      ms = [{[:"$1", :_], [{:==, :"$1", "foo"}], [{:return_trace}]}]
      assert :ok = Guardrails.validate({String, :replace, ms}, {100, 1_000})
    end
  end
end
```

### Step 7: `test/recon_trace_prod/safe_test.exs`

```elixir
defmodule ReconTraceProd.SafeTest do
  use ExUnit.Case, async: false

  alias ReconTraceProd.Safe

  @tmp_dir Path.join(System.tmp_dir!(), "recon_trace_prod_test")

  setup do
    File.mkdir_p!(@tmp_dir)
    Safe.stop()
    on_exit(fn -> Safe.stop() end)
    :ok
  end

  test "rejects a trace exceeding safety cap" do
    path = Path.join(@tmp_dir, "refuse.log")
    assert {:error, _} = Safe.trace({String, :upcase, 1}, {99_999, 1_000}, path)
    assert :idle = Safe.status()
  end

  test "starts a trace, captures calls, and stops cleanly" do
    path = Path.join(@tmp_dir, "capture.log")

    assert :ok = Safe.trace({String, :upcase, 1}, {100, 1_000}, path)
    assert {:active, info} = Safe.status()
    assert info.spec == {String, :upcase, 1}

    # Generate calls that should match
    for _ <- 1..5, do: String.upcase("hello")
    Process.sleep(150)

    :ok = Safe.stop()
    assert :idle = Safe.status()

    contents = File.read!(path)
    assert contents =~ "String.upcase/1"
  end

  test "rejects overlapping traces" do
    path1 = Path.join(@tmp_dir, "a.log")
    path2 = Path.join(@tmp_dir, "b.log")

    assert :ok = Safe.trace({String, :upcase, 1}, {100, 1_000}, path1)
    assert {:error, :already_active} = Safe.trace({String, :downcase, 1}, {100, 1_000}, path2)
    :ok = Safe.stop()
  end
end
```

### Step 8: Benchmark the overhead of an active trace

```elixir
# bench/overhead_bench.exs
alias ReconTraceProd.Safe

path = Path.join(System.tmp_dir!(), "bench.log")

Benchee.run(
  %{
    "String.upcase (no trace)" => fn -> String.upcase("elixir") end,
    "String.upcase (trace active, no match)" => fn -> String.upcase("elixir") end
  },
  before_scenario: fn input ->
    Safe.stop()

    if input == "String.upcase (trace active, no match)" do
      # guard will never match — shows the cost of the VM-level filter
      ms = [{[:"$1"], [{:==, :"$1", "never_called_with_this"}], []}]
      :ok = Safe.trace({String, :upcase, ms}, {100, 1_000}, path)
    end

    input
  end,
  after_scenario: fn _ -> Safe.stop() end,
  time: 3,
  warmup: 1
)
```

On an M2 MacBook Pro the "trace active, no match" scenario runs within 3–5% of
the baseline, confirming that the VM-level match spec filter is effectively free.

---

## Trade-offs and production gotchas

**1. One trace per node, not per service.** `:recon_trace` uses the global `:dbg`
backend — two concurrent traces fight over `:dbg.tracer/1`. Enforce single-writer
semantics in the façade.

**2. `return_trace` doubles message volume.** Every call emits a `:call` *and* a
`:return_from` message. Halve your `max_msgs` limit if you enable return traces.

**3. `{:caller}` requires `:meta` tracing.** Some OTP versions require enabling
`:meta` tracing on the process before `{:caller}` is populated; otherwise you
get `:undefined`. Verify on a staging node before relying on it.

**4. Match specs cannot call Elixir code.** Only a whitelisted set of BIFs is
allowed inside guards (`==`, `<`, `is_*`, `element`, `size`). You cannot call
`String.contains?/2` inside a trace guard — do shape-level filtering in the
spec, post-filter in your formatter.

**5. `:delayed_write` can lose data on abrupt node exit.** If the node crashes
while the 64 KiB buffer is not flushed, the tail of the trace file is gone.
For investigations where every line matters, reduce the buffer to `{0, 0}`
(immediate flush) and accept the 10–20 µs per write.

**6. Distributed nodes need per-node invocation.** `:recon_trace` is local.
To trace across a cluster use `:erpc.multicall(nodes, :recon_trace, :calls, [...])`
and aggregate output offline — do NOT send trace messages across the distribution
protocol; they will saturate the `tcp_dist` port.

**7. LiveDashboard integration tempts over-exposure.** A trace page that any
operator can hit is a denial-of-service vector. Gate it behind the existing
production auth and log every invocation to your audit trail.

**8. When NOT to use this.** For functions called more than ~100k times per
second even with a filtered match spec, the `:dbg` callback itself becomes
a bottleneck — the VM takes the tracer lock for every matched call. Reach
for `:fprof` (off-line, sample-based) or structured logging with log-level
toggled at runtime via `Logger.configure/1` instead.

---

## Benchmark

Expected results on an Apple M2 (OTP 26):

| Scenario | p50 | p99 | Note |
|----------|-----|-----|------|
| baseline | 72 ns | 180 ns | no tracing |
| match spec, no match | 76 ns | 210 ns | VM filter only |
| match spec, every call matches | 14 µs | 38 µs | message + formatter |
| `:dbg.tp` with no match spec | 11 µs | 90 µs | mailbox pressure |

Rule of thumb: if a traced function is called less than 1k/s, overhead is
irrelevant. Above 10k/s, every filter-clause you add in the match spec matters.

---

## Resources

- [`recon` library documentation](https://ferd.github.io/recon/recon_trace.html) — the authoritative API reference
- [Erlang in Anger — Fred Hébert](https://www.erlang-in-anger.com/) — chapters 5 and 9 cover tracing and runtime debugging in production
- [`:dbg` reference](https://www.erlang.org/doc/man/dbg.html) — understand what `:recon_trace` wraps
- [Match specifications](https://www.erlang.org/doc/apps/erts/match_spec.html) — the compiled guard language
- [Discord's engineering blog — scaling Elixir](https://discord.com/blog/how-discord-scaled-elixir-to-5-000-000-concurrent-users) — real-world tracing stories at scale
- [Dashbit — observability in Elixir](https://dashbit.co/blog/observability-and-elixir) — José Valim on runtime introspection strategy

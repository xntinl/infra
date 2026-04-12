# Supervision Tree with `:rest_for_one` and Custom Restart Intensity

**Project**: `ingest_pipeline` — three-stage pipeline (Source → Transform → Sink) where a failure in an upstream stage must restart itself and everything downstream, with custom `max_restarts`/`max_seconds` tuned to tolerate spiky upstream flakiness without flapping.

## Project context

You run an ingestion pipeline: an HTTP-polling `Source`, a `Transform` process that normalises payloads, and a `Sink` that writes to a Postgres journal. The stages share in-memory subscriptions: `Transform` subscribes to `Source`, `Sink` subscribes to `Transform`. When `Source` dies, `Transform` has stale subscription state and `Sink` is feeding from nothing — both need to restart too. When `Sink` dies, only `Sink` needs to restart; `Source` and `Transform` still have valid state upstream.

The BEAM has a built-in supervision strategy exactly for this shape: `:rest_for_one`. Children are ordered. When child *N* dies, children *N, N+1, N+2, ...* restart in order. Earlier children survive. It is neither `:one_for_one` (too laissez-faire; downstream is left with stale state) nor `:one_for_all` (too aggressive; restarts the Source even when only the Sink failed).

The other axis is *how many restarts* are tolerated in *how much time*. Default OTP values (`max_restarts: 3, max_seconds: 5`) were picked for small embedded apps; for a pipeline subject to intermittent upstream hiccups, 3-in-5 is a flapping recipe. Tune both.

```
ingest_pipeline/
├── lib/
│   └── ingest_pipeline/
│       ├── application.ex
│       ├── supervisor.ex              # rest_for_one with tuned intensity
│       ├── source.ex                  # polls HTTP
│       ├── transform.ex               # normalises
│       └── sink.ex                    # writes journal
├── test/
│   └── ingest_pipeline/
│       └── supervisor_test.exs
├── bench/
│   └── restart_storm_bench.exs
└── mix.exs
```

## Why `:rest_for_one` and not the alternatives

| Strategy | When child N dies, what happens |
|---|---|
| `:one_for_one` | Only N restarts. Peers continue with stale references to the dead child. |
| `:one_for_all` | All siblings restart. Wasteful if only N had a real fault. |
| `:rest_for_one` | N, N+1, N+2, ... restart. Upstream (earlier) survives. |
| `:simple_one_for_one` (deprecated) | Replaced by `DynamicSupervisor`; irrelevant here. |

For a linear pipeline, `:rest_for_one` is the natural fit.

## Why tune `max_restarts` and `max_seconds`

The default pair means "if 3 restarts happen within 5 seconds, the supervisor itself dies and the parent gets to decide". That is conservative — great for embedded, wrong for an ingest pipeline that may see 10 HTTP hiccups per minute from a flaky upstream. Tune based on two numbers:

1. **Expected transient failure rate** at peak (e.g. 1 fail per minute under flaky upstream).
2. **Acceptable mean time to repair** if a persistent failure happens (seconds of outage before the supervisor gives up and escalates).

Rule of thumb: set `max_restarts` so that the expected transient rate is clearly under it, and `max_seconds` to the window within which the transient rate applies. Going to 10/60 (ten restarts per minute) is often right for pipelines.

## Core concepts

### 1. Supervisor strategy
Determines which children restart when one dies.

### 2. Restart intensity
`max_restarts` over `max_seconds` — if exceeded, the supervisor exits with `:shutdown` and its own parent gets to decide.

### 3. Child spec `:restart` option
`:permanent` (always restart), `:transient` (restart only on abnormal exit), `:temporary` (never restart).

### 4. Child ordering
With `:rest_for_one`, order is semantic: upstream first, downstream last.

### 5. Escalation
When the supervisor itself exits (because restart intensity was exceeded), the *parent* supervisor handles it. Nested supervisors are how you get different restart policies for different subsystems.

## Design decisions

- **Option A — all three stages under one `:rest_for_one` supervisor**: clean, matches the dependency graph.
- **Option B — two `:one_for_one` supervisors, with message-based dependency**: Source is independent; Transform subscribes; Sink subscribes. Con: you have to handle the "subscription target died" event yourself in application code.

→ A. BEAM supervision semantics line up with the real dependency. Why reimplement it in application code?

- **Option A — permanent for all stages**: any death restarts.
- **Option B — transient for Sink**: Sink exits `:normal` when it drains; no restart.

→ Depends on the domain. Here all three are permanent (they are long-lived loops). We revisit if we add a one-shot batch job.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps, do: [{:benchee, "~> 1.3", only: [:dev, :test]}]
```

### Step 1: The three stages

```elixir
defmodule IngestPipeline.Source do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def crash, do: GenServer.cast(__MODULE__, :crash)
  def state, do: GenServer.call(__MODULE__, :state)

  @impl true
  def init(_opts) do
    :logger.info("Source starting")
    {:ok, %{polled: 0, started_at: System.monotonic_time()}}
  end

  @impl true
  def handle_call(:state, _from, state), do: {:reply, state, state}

  @impl true
  def handle_cast(:crash, _state), do: raise("source upstream is on fire")
end

defmodule IngestPipeline.Transform do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def crash, do: GenServer.cast(__MODULE__, :crash)
  def state, do: GenServer.call(__MODULE__, :state)

  @impl true
  def init(_) do
    # Subscribe to the Source — if Source restarts, we want to re-subscribe.
    ref = Process.monitor(IngestPipeline.Source)
    {:ok, %{source_ref: ref, transformed: 0}}
  end

  @impl true
  def handle_call(:state, _from, state), do: {:reply, state, state}

  @impl true
  def handle_cast(:crash, _state), do: raise("transform blew up")

  @impl true
  def handle_info({:DOWN, _ref, :process, _pid, _reason}, _state) do
    # Our supervisor will restart us too (rest_for_one). Exit cleanly.
    {:stop, :shutdown, nil}
  end
end

defmodule IngestPipeline.Sink do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def crash, do: GenServer.cast(__MODULE__, :crash)
  def state, do: GenServer.call(__MODULE__, :state)

  @impl true
  def init(_) do
    ref = Process.monitor(IngestPipeline.Transform)
    {:ok, %{transform_ref: ref, written: 0}}
  end

  @impl true
  def handle_call(:state, _from, state), do: {:reply, state, state}

  @impl true
  def handle_cast(:crash, _state), do: raise("sink Postgres down")

  @impl true
  def handle_info({:DOWN, _ref, :process, _pid, _reason}, _state) do
    {:stop, :shutdown, nil}
  end
end
```

### Step 2: The supervisor

```elixir
defmodule IngestPipeline.Supervisor do
  use Supervisor

  def start_link(opts), do: Supervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    children = [
      %{id: IngestPipeline.Source,    start: {IngestPipeline.Source,    :start_link, [[]]}, restart: :permanent},
      %{id: IngestPipeline.Transform, start: {IngestPipeline.Transform, :start_link, [[]]}, restart: :permanent},
      %{id: IngestPipeline.Sink,      start: {IngestPipeline.Sink,      :start_link, [[]]}, restart: :permanent}
    ]

    # Tuned: we tolerate up to 10 restarts per 60s before escalating.
    # Rationale: at 1 transient failure/min under flaky upstream, we still have
    # 5× headroom; a persistent fault (10 failures in a minute) correctly escalates.
    Supervisor.init(children,
      strategy: :rest_for_one,
      max_restarts: 10,
      max_seconds: 60
    )
  end
end
```

### Step 3: Application

```elixir
defmodule IngestPipeline.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [IngestPipeline.Supervisor]
    Supervisor.start_link(children, strategy: :one_for_one, name: IngestPipeline.Root)
  end
end
```

## Restart behaviour diagram

```
          ─── rest_for_one ───
          ▼                  ▼
  ┌───────────┐   ┌───────────┐   ┌───────────┐
  │  Source   │──▶│ Transform │──▶│   Sink    │
  └───────────┘   └───────────┘   └───────────┘

CASE 1: Sink dies
                                     │
                                     ▼ crash
  ┌───────────┐   ┌───────────┐   ┌───────────┐
  │  Source   │   │ Transform │   │ Sink (new)│
  └───────────┘   └───────────┘   └───────────┘
  ^ unchanged     ^ unchanged     ^ restarted only

CASE 2: Transform dies
                       │
                       ▼ crash
  ┌───────────┐   ┌───────────┐   ┌───────────┐
  │  Source   │   │ Transform │──▶│ Sink (new)│
  │  (unchg)  │   │  (new)    │   │  (new)    │
  └───────────┘   └───────────┘   └───────────┘
                  ^ restart        ^ cascades because rest_for_one

CASE 3: Source dies
       │
       ▼ crash
  ┌───────────┐   ┌───────────┐   ┌───────────┐
  │  Source   │──▶│ Transform │──▶│ Sink (new)│
  │   (new)   │   │   (new)   │   │   (new)   │
  └───────────┘   └───────────┘   └───────────┘
  ^ restart       ^ restart       ^ restart
```

## Tests

```elixir
defmodule IngestPipeline.SupervisorTest do
  use ExUnit.Case, async: false

  alias IngestPipeline.{Supervisor, Source, Transform, Sink}

  setup do
    start_supervised!(Supervisor)
    # Wait for processes to fully register.
    :ok = wait_for_registered([Source, Transform, Sink])
    :ok
  end

  describe "rest_for_one semantics" do
    test "Sink crash does not affect Source or Transform" do
      src_pid = Process.whereis(Source)
      xfm_pid = Process.whereis(Transform)
      snk_pid = Process.whereis(Sink)

      Process.flag(:trap_exit, true)
      catch_exit(Sink.crash())

      :ok = wait_until(fn ->
        new_snk = Process.whereis(Sink)
        is_pid(new_snk) and new_snk != snk_pid
      end)

      assert Process.whereis(Source) == src_pid
      assert Process.whereis(Transform) == xfm_pid
    end

    test "Transform crash restarts Transform and Sink, not Source" do
      src_pid = Process.whereis(Source)
      xfm_pid = Process.whereis(Transform)
      snk_pid = Process.whereis(Sink)

      Process.flag(:trap_exit, true)
      catch_exit(Transform.crash())

      :ok = wait_until(fn ->
        new_xfm = Process.whereis(Transform)
        new_snk = Process.whereis(Sink)
        is_pid(new_xfm) and new_xfm != xfm_pid and is_pid(new_snk) and new_snk != snk_pid
      end)

      assert Process.whereis(Source) == src_pid
    end

    test "Source crash cascades to all three" do
      src_pid = Process.whereis(Source)
      xfm_pid = Process.whereis(Transform)
      snk_pid = Process.whereis(Sink)

      Process.flag(:trap_exit, true)
      catch_exit(Source.crash())

      :ok = wait_until(fn ->
        is_pid(Process.whereis(Source)) and Process.whereis(Source) != src_pid and
          is_pid(Process.whereis(Transform)) and Process.whereis(Transform) != xfm_pid and
          is_pid(Process.whereis(Sink)) and Process.whereis(Sink) != snk_pid
      end)
    end
  end

  # --- helpers ---

  defp wait_for_registered(names) do
    wait_until(fn -> Enum.all?(names, &is_pid(Process.whereis(&1))) end)
  end

  defp wait_until(fun, deadline \\ 500) do
    start = System.monotonic_time(:millisecond)
    do_wait(fun, start, deadline)
  end

  defp do_wait(fun, start, deadline) do
    cond do
      fun.() -> :ok
      System.monotonic_time(:millisecond) - start > deadline -> flunk("condition never became true")
      true ->
        Process.sleep(10)
        do_wait(fun, start, deadline)
    end
  end
end
```

## Benchmark

The "benchmark" here is operational, not throughput: it measures how long the pipeline spends **rebuilding state** after a cascade.

```elixir
# bench/restart_storm_bench.exs
# Start the supervisor, then crash Source and measure until all three are up again.
start_supervised!(IngestPipeline.Supervisor)

{time_us, _} =
  :timer.tc(fn ->
    Process.flag(:trap_exit, true)
    try do
      IngestPipeline.Source.crash()
    catch
      :exit, _ -> :ok
    end

    # Wait until all three processes are alive with new pids.
    wait = fn f ->
      if Process.whereis(IngestPipeline.Source) && Process.whereis(IngestPipeline.Transform) &&
           Process.whereis(IngestPipeline.Sink) do
        :ok
      else
        Process.sleep(1)
        f.(f)
      end
    end

    wait.(wait)
  end)

IO.puts("Recovery time: #{time_us}µs (#{Float.round(time_us / 1000, 2)}ms)")
```

Expected: 5–30ms for three GenServers with empty `init/1`. If `init/1` opens DB connections, multiply by the connection handshake cost. Long recovery times (> 500ms) are your signal that `init/1` is doing too much — push slow work into `handle_continue/2`.

## Trade-offs and production gotchas

**1. `max_restarts` too low → flapping**
With default 3/5, a persistently flaky upstream causes the supervisor to give up after 3 failures, escalating to its parent. The parent may restart you, which resets the counter. Net effect: you flap between running and restarting. Either bump the intensity or add circuit-breaking upstream.

**2. `max_restarts` too high → death loop hides outage**
If you set 1000/60, the supervisor will silently thrash. You lose the alarm that "something is wrong". The intensity must be tuned *so that persistent failures do escalate*.

**3. `init/1` that blocks**
If `Transform.init/1` opens a TCP connection that takes 2 seconds, then during a cascade restart the whole supervisor blocks for 6 seconds (three sequential inits). Move slow work to `handle_continue/2`.

**4. Child ordering is semantic**
Swap the order of `Source` and `Sink` by accident and `rest_for_one` cascades in the wrong direction. Comment every child spec with what depends on what.

**5. `Process.monitor` vs link**
Inside a supervised tree, the supervisor links its children. Don't *also* link your stages to each other — that creates redundant death propagation. Use `Process.monitor/1` between stages if you need to observe; the supervisor owns liveness.

**6. When NOT to use `:rest_for_one`**
When your processes are peers (not a pipeline), `:one_for_one` is better — the dependency graph isn't linear. When everything shares state (e.g. a leader election where any death invalidates cluster state), `:one_for_all` is correct.

## Reflection

Your supervisor is configured at `10 restarts / 60s`. Imagine a persistent upstream fault that makes `Source` crash every 6 seconds exactly. The supervisor restarts 10 times in 60 seconds — at the 10th, it exceeds intensity and the supervisor itself exits. What happens next? Trace the escalation through `IngestPipeline.Application`. Would you prefer the node to crash (`:kernel` brings it down), or to survive and retry from the top? How would you express that choice?

## Resources

- [`Supervisor` module docs](https://hexdocs.pm/elixir/Supervisor.html)
- [OTP Design Principles — Supervision Principles](https://www.erlang.org/doc/design_principles/sup_princ.html)
- [Fred Hebert — "Stuff Goes Bad: Erlang in Anger"](https://www.erlang-in-anger.com/) — chapter on restart strategies
- [José Valim — "The Road to 2 Million WebSockets" (talk)](https://www.youtube.com/watch?v=6pYUKYiD5s8)

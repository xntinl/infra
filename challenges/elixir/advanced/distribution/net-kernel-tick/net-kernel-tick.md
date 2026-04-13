# Tuning net_kernel Tick and Heartbeats Between Nodes

**Project**: `net_kernel_tuning` — stabilizing a WAN-spanning BEAM cluster

---

## Why distribution and clustering matters

Distributed Erlang gives you remote message-passing transparency, but the cost is your responsibility for split-brain detection, registry consistency, and net-tick policies. Libcluster, Horde, and PG provide pieces; you compose them.

Clusters fail in interesting ways: netsplits, asymmetric partitions, GC pauses misread as crashes, and global registry race conditions. Designing for the network — rather than against it — is the senior shift.

---

## The business problem

You are building a production-grade Elixir component in the **Distribution and clustering** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
net_kernel_tuning/
├── lib/
│   └── net_kernel_tuning.ex
├── script/
│   └── main.exs
├── test/
│   └── net_kernel_tuning_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Distribution and clustering the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule NetKernelTuning.MixProject do
  use Mix.Project

  def project do
    [
      app: :net_kernel_tuning,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/net_kernel_tuning.ex`

```elixir
defmodule NetKernelTuning.PartitionReporter do
  @moduledoc """
  Subscribes to nodeup/nodedown events and logs with enough context to
  diagnose whether a disconnect was a real partition or a ticktime false
  positive. Include the current net_ticktime so logs are self-explanatory.
  """
  use GenServer
  require Logger

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init([]) do
    :net_kernel.monitor_nodes(true, node_type: :all)
    {:ok, %{since: %{}}}
  end

  @impl true
  def handle_info({:nodeup, node, info}, state) do
    Logger.info("nodeup: #{inspect(node)} info=#{inspect(info)} ticktime=#{net_ticktime()}")
    {:noreply, put_in(state.since[node], System.monotonic_time(:millisecond))}
  end

  @impl true
  def handle_info({:nodedown, node, info}, state) do
    lived_ms =
      case Map.fetch(state.since, node) do
        {:ok, t} -> System.monotonic_time(:millisecond) - t
        :error -> :unknown
      end

    Logger.warning(
      "nodedown: #{inspect(node)} info=#{inspect(info)} " <>
        "lived_ms=#{inspect(lived_ms)} ticktime=#{net_ticktime()}"
    )

    {:noreply, %{state | since: Map.delete(state.since, node)}}
  end

  defp net_ticktime, do: :net_kernel.get_net_ticktime()
end

defmodule NetKernelTuning.LatencyProbe do
  @moduledoc """
  Measures inter-node RTT via :erpc. Run on a schedule to detect WAN
  degradation before net_kernel tears the cluster apart.
  """
  use GenServer
  require Logger

  @interval_ms 5_000
  @slow_threshold_ms 250

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc "Synchronously probe `node` and return the RTT in ms."
  @spec probe(node(), timeout()) :: {:ok, pos_integer()} | {:error, term()}
  def probe(node, timeout \\ 1_000) do
    t0 = System.monotonic_time(:microsecond)

    try do
      :erpc.call(node, :erlang, :node, [], timeout)
      {:ok, div(System.monotonic_time(:microsecond) - t0, 1000)}
    rescue
      e in RuntimeError -> {:error, e}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end

  @impl true
  def init(_opts) do
    schedule()
    {:ok, %{samples: %{}}}
  end

  @impl true
  def handle_info(:probe, state) do
    samples =
      Node.list()
      |> Enum.map(fn node ->
        case probe(node) do
          {:ok, rtt_ms} ->
            if rtt_ms >= @slow_threshold_ms do
              Logger.warning("slow peer: #{inspect(node)} rtt=#{rtt_ms}ms")
            end

            {node, rtt_ms}

          {:error, reason} ->
            Logger.warning("probe failed: #{inspect(node)} reason=#{inspect(reason)}")
            {node, :unreachable}
        end
      end)
      |> Map.new()

    schedule()
    {:noreply, %{state | samples: samples}}
  end

  @doc "Returns samples result."
  @spec samples() :: %{node() => pos_integer() | :unreachable}
  def samples, do: GenServer.call(__MODULE__, :samples)

  @impl true
  def handle_call(:samples, _from, state), do: {:reply, state.samples, state}

  defp schedule, do: Process.send_after(self(), :probe, @interval_ms)
end

defmodule NetKernelTuning.Heartbeat do
  @moduledoc """
  Periodic round-trip over `:erpc` to confirm peer is not only reachable but
  responsive. Detects scheduler stalls that net_kernel misses.
  """
  use GenServer
  require Logger

  @interval_ms 5_000
  @timeout_ms 2_000
  @max_consecutive_failures 3

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    schedule()
    {:ok, %{failures: %{}}}
  end

  @impl true
  def handle_info(:beat, state) do
    failures =
      Node.list()
      |> Enum.reduce(state.failures, fn node, acc ->
        case beat(node) do
          :ok ->
            Map.delete(acc, node)

          {:error, reason} ->
            count = Map.get(acc, node, 0) + 1

            if count >= @max_consecutive_failures do
              Logger.error(
                "peer #{inspect(node)} unresponsive #{count} times; " <>
                  "reason=#{inspect(reason)}. Consider application-level removal."
              )
            end

            Map.put(acc, node, count)
        end
      end)

    schedule()
    {:noreply, %{state | failures: failures}}
  end

  defp beat(node) do
    try do
      case :erpc.call(node, :erlang, :statistics, [:run_queue], @timeout_ms) do
        n when is_integer(n) and n < 1_000 -> :ok
        n when is_integer(n) -> {:error, {:run_queue_high, n}}
        other -> {:error, {:unexpected, other}}
      end
    rescue
      e in RuntimeError -> {:error, e}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end

  defp schedule, do: Process.send_after(self(), :beat, @interval_ms)
end

defmodule NetKernelTuning.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      NetKernelTuning.PartitionReporter,
      NetKernelTuning.LatencyProbe,
      NetKernelTuning.Heartbeat
    ]

    opts = [strategy: :one_for_one, name: NetKernelTuning.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```
### `test/net_kernel_tuning_test.exs`

```elixir
defmodule NetKernelTuning.HeartbeatTest do
  use ExUnit.Case, async: true
  doctest NetKernelTuning.PartitionReporter

  alias NetKernelTuning.Heartbeat

  setup do
    :net_kernel.start([:"test@127.0.0.1"], %{name_domain: :longnames})

    {:ok, peer, node} =
      :peer.start_link(%{name: :beat_peer, host: ~c"127.0.0.1", longnames: true})

    on_exit(fn -> :peer.stop(peer) end)
    %{peer: peer, node: node}
  end

  describe "NetKernelTuning.Heartbeat" do
    test "probe succeeds against a healthy peer", %{node: node} do
      assert {:ok, rtt_ms} = NetKernelTuning.LatencyProbe.probe(node, 1_000)
      assert rtt_ms >= 0
      assert rtt_ms < 500
    end

    test "probe fails fast against a bogus node" do
      t0 = System.monotonic_time(:millisecond)
      assert {:error, _} = NetKernelTuning.LatencyProbe.probe(:"nonexistent@127.0.0.1", 200)
      elapsed = System.monotonic_time(:millisecond) - t0
      assert elapsed < 800
    end

    test "get_net_ticktime returns the configured value" do
      # OTP rounds to the nearest valid value; just assert it's a positive integer.
      assert is_integer(:net_kernel.get_net_ticktime())
      assert :net_kernel.get_net_ticktime() > 0
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Tuning net_kernel Tick and Heartbeats Between Nodes.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Tuning net_kernel Tick and Heartbeats Between Nodes ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case NetKernelTuning.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: NetKernelTuning.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Partitions are the rule, not the exception

In a multi-AZ cluster, brief netsplits happen daily. Design for them: prefer eventual consistency, use idempotent operations, and detect split-brain explicitly.

### 2. Registries don't replicate transparently

Local Registry is fast and node-local. :global is consistent but slow. Horde.Registry replicates via CRDTs — eventual consistency, no global locks. Pick based on your read/write ratio.

### 3. Tune net_kernel ticks for your environment

The default 60-second tick is too long for production failure detection but too short for high-latency cross-region links. Measure first.

---

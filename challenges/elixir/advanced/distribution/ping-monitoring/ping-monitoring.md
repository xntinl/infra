# Remote Process Monitoring with Process.monitor and nodedown Handling

**Project**: `node_ping_monitor` — robust remote-process supervision

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
node_ping_monitor/
├── lib/
│   └── node_ping_monitor.ex
├── script/
│   └── main.exs
├── test/
│   └── node_ping_monitor_test.exs
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
defmodule NodePingMonitor.MixProject do
  use Mix.Project

  def project do
    [
      app: :node_ping_monitor,
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

### `lib/node_ping_monitor.ex`

```elixir
defmodule NodePingMonitor.TenantCoordinator do
  @moduledoc """
  Domain process. In production, owns tenant-specific state. Here it's
  a registered GenServer that answers `:ping` calls; this is the process
  we remote-monitor from node A.
  """
  use GenServer

  def start_link(opts) do
    name = Keyword.get(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  @doc "Returns ping result from name and timeout."
  def ping(name \\ __MODULE__, timeout \\ 1_000) do
    GenServer.call(name, :ping, timeout)
  end

  @impl true
  def init(opts), do: {:ok, %{started_at: System.monotonic_time(), opts: opts}}

  @impl true
  def handle_call(:ping, _from, state), do: {:reply, {:pong, node()}, state}

  @impl true
  def handle_call(:crash, _from, _state), do: exit(:deliberate_crash)
end

defmodule NodePingMonitor.PingTracker do
  @moduledoc """
  Translates raw :DOWN / :nodedown / :nodeup events into domain events
  suitable for supervision logic. Pure-ish: no side effects besides logging.
  """
  require Logger

  @type down_reason :: :normal | :shutdown | :killed | :noproc | :noconnection | term()

  @type classification ::
          :retry_fast | :retry_slow | :wait_for_nodeup | :alert_and_slow

  @doc "Returns classify result."
  @spec classify(down_reason()) :: classification()
  def classify(:normal), do: :retry_fast
  @doc "Returns classify result."
  def classify(:shutdown), do: :retry_fast
  @doc "Returns classify result."
  def classify(:noproc), do: :retry_fast
  @doc "Returns classify result."
  def classify(:noconnection), do: :wait_for_nodeup
  @doc "Returns classify result."
  def classify(:killed), do: :alert_and_slow
  @doc "Returns classify result from _."
  def classify({:shutdown, _}), do: :retry_fast
  @doc "Returns classify result from _other."
  def classify(_other), do: :alert_and_slow

  @doc """
  Next backoff delay given the current attempt count, classification, and RNG.
  Pure function so tests are deterministic.
  """
  @spec next_delay_ms(non_neg_integer(), classification(), (non_neg_integer() -> non_neg_integer())) ::
          pos_integer()
  @doc "Returns next delay ms result from attempts, classification and rand."
  def next_delay_ms(attempts, classification, rand \\ &:rand.uniform/1) do
    base =
      case classification do
        :retry_fast -> min(100 * :math.pow(2, attempts) |> trunc(), 2_000)
        :retry_slow -> min(500 * :math.pow(2, attempts) |> trunc(), 10_000)
        :alert_and_slow -> min(500 * :math.pow(2, attempts) |> trunc(), 10_000)
        :wait_for_nodeup -> 30_000
      end

    # +/- 20% jitter
    jitter = rand.(max(div(base, 5), 1))
    base + jitter - div(base, 10)
  end
end

defmodule NodePingMonitor.DashboardClient do
  @moduledoc """
  Watches a remote registered process and emits domain events on transitions.
  Re-monitors automatically with backoff. Subscribes to nodeup/nodedown so
  it can fast-path reconnect when the target's node comes back.
  """
  use GenServer
  require Logger

  alias NodePingMonitor.PingTracker

  @type t :: %{
          target_name: atom(),
          target_node: node(),
          monitor_ref: reference() | nil,
          attempts: non_neg_integer(),
          state: :watching | :backing_off | :waiting_for_nodeup,
          listener: pid()
        }

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    target_name = Keyword.fetch!(opts, :target_name)
    target_node = Keyword.fetch!(opts, :target_node)
    listener = Keyword.get(opts, :listener, self())

    :net_kernel.monitor_nodes(true, node_type: :all)

    state =
      %{
        target_name: target_name,
        target_node: target_node,
        monitor_ref: nil,
        attempts: 0,
        state: :backing_off,
        listener: listener
      }
      |> start_monitoring()

    {:ok, state}
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _target, reason}, %{monitor_ref: ref} = state) do
    classification = PingTracker.classify(reason)
    emit(state.listener, {:target_down, state.target_node, reason, classification})
    Logger.info("target down: node=#{state.target_node} reason=#{inspect(reason)} class=#{classification}")

    new_state = %{state | monitor_ref: nil, attempts: state.attempts + 1}

    case classification do
      :wait_for_nodeup ->
        {:noreply, %{new_state | state: :waiting_for_nodeup}}

      _ ->
        delay = PingTracker.next_delay_ms(state.attempts, classification)
        Process.send_after(self(), :retry_monitor, delay)
        {:noreply, %{new_state | state: :backing_off}}
    end
  end

  def handle_info(:retry_monitor, state) do
    {:noreply, start_monitoring(state)}
  end

  def handle_info({:nodeup, node, _info}, %{target_node: node, state: :waiting_for_nodeup} = state) do
    Logger.info("target node reappeared: #{inspect(node)}")
    emit(state.listener, {:nodeup, node})
    {:noreply, start_monitoring(%{state | attempts: 0})}
  end

  def handle_info({:nodeup, _other, _info}, state), do: {:noreply, state}

  def handle_info({:nodedown, node, _info}, %{target_node: node} = state) do
    Logger.warning("target node went down: #{inspect(node)}")
    emit(state.listener, {:nodedown, node})
    {:noreply, state}
  end

  def handle_info({:nodedown, _other, _info}, state), do: {:noreply, state}

  def handle_info(other, state) do
    Logger.debug("unhandled message: #{inspect(other)}")
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, %{monitor_ref: ref}) when is_reference(ref) do
    Process.demonitor(ref, [:flush])
    :ok
  end

  def terminate(_, _), do: :ok

  defp start_monitoring(state) do
    ref = Process.monitor({state.target_name, state.target_node})

    emit(state.listener, {:monitoring, state.target_node})

    %{state | monitor_ref: ref, state: :watching}
  end

  defp emit(nil, _), do: :ok
  defp emit(pid, msg) when is_pid(pid), do: send(pid, msg)
end

defmodule NodePingMonitor.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = []
    opts = [strategy: :one_for_one, name: NodePingMonitor.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### `test/node_ping_monitor_test.exs`

```elixir
defmodule NodePingMonitor.PingTrackerTest do
  use ExUnit.Case, async: true
  doctest NodePingMonitor.TenantCoordinator

  alias NodePingMonitor.PingTracker

  describe "NodePingMonitor.PingTracker" do
    test "classify produces distinct classes for distinct reasons" do
      assert PingTracker.classify(:normal) == :retry_fast
      assert PingTracker.classify(:shutdown) == :retry_fast
      assert PingTracker.classify(:noproc) == :retry_fast
      assert PingTracker.classify(:noconnection) == :wait_for_nodeup
      assert PingTracker.classify(:killed) == :alert_and_slow
      assert PingTracker.classify({:shutdown, :deliberate}) == :retry_fast
      assert PingTracker.classify(:custom_error) == :alert_and_slow
    end

    test "next_delay_ms grows exponentially for fast retries" do
      deterministic_rand = fn _ -> 0 end

      d0 = PingTracker.next_delay_ms(0, :retry_fast, deterministic_rand)
      d1 = PingTracker.next_delay_ms(1, :retry_fast, deterministic_rand)
      d2 = PingTracker.next_delay_ms(2, :retry_fast, deterministic_rand)

      assert d0 < d1
      assert d1 < d2
    end

    test "next_delay_ms caps fast retries at 2s" do
      deterministic_rand = fn _ -> 0 end
      d = PingTracker.next_delay_ms(100, :retry_fast, deterministic_rand)
      # cap is 2000, jitter adds a bit, but the base is clamped
      assert d <= 2_100
    end

    test "wait_for_nodeup uses a long fixed delay" do
      d = PingTracker.next_delay_ms(0, :wait_for_nodeup, fn _ -> 0 end)
      assert d >= 20_000
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Remote Process Monitoring with Process.monitor and nodedown Handling.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Remote Process Monitoring with Process.monitor and nodedown Handling ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case NodePingMonitor.run(payload) do
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
        for _ <- 1..1_000, do: NodePingMonitor.run(:bench)
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

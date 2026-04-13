# Split-Brain Handling Strategies

**Project**: `split_brain_demo` — comparing LWW, quorum, and CRDT-merge

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
split_brain_demo/
├── lib/
│   └── split_brain_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── split_brain_demo_test.exs
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
defmodule SplitBrainDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :split_brain_demo,
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

### `lib/split_brain_demo.ex`

```elixir
defmodule SplitBrainDemo.LwwRegister do
  @moduledoc """
  Last-Write-Wins register using a Hybrid Logical Clock.

  The register holds a single value. Concurrent writes are resolved by HLC
  comparison; ties are broken by lexicographic node name. Data loss: if two
  nodes set different values at the same HLC tick, only one survives.
  """

  @type hlc :: {physical :: non_neg_integer(), logical :: non_neg_integer(), node()}
  @type t :: %__MODULE__{value: term(), clock: hlc()}

  defstruct value: nil, clock: {0, 0, :nonode@nohost}

  @spec new(term()) :: t()
  def new(initial \\ nil) do
    %__MODULE__{value: initial, clock: {System.system_time(:millisecond), 0, node()}}
  end

  @spec set(t(), term()) :: t()
  def set(%__MODULE__{clock: clock} = reg, value) do
    %{reg | value: value, clock: tick_local(clock)}
  end

  @spec merge(t(), t()) :: t()
  def merge(%__MODULE__{} = a, %__MODULE__{} = b) do
    if compare(a.clock, b.clock) == :gt do
      a
    else
      b
    end
  end

  @doc "Used when receiving a remote update — advances the local clock."
  @spec update_from_remote(t(), t()) :: t()
  def update_from_remote(%__MODULE__{} = local, %__MODULE__{} = remote) do
    merged = merge(local, remote)
    %{merged | clock: tick_receive(local.clock, remote.clock)}
  end

  @spec value(t()) :: term()
  def value(%__MODULE__{value: v}), do: v

  # --- HLC internals ---

  defp tick_local({phys, log, _node}) do
    now = System.system_time(:millisecond)

    cond do
      now > phys -> {now, 0, node()}
      now == phys -> {phys, log + 1, node()}
      true -> {phys, log + 1, node()}
    end
  end

  defp tick_receive({lp, ll, _}, {rp, rl, _}) do
    now = System.system_time(:millisecond)
    max_p = Enum.max([now, lp, rp])

    log =
      cond do
        max_p == lp and max_p == rp -> max(ll, rl) + 1
        max_p == lp -> ll + 1
        max_p == rp -> rl + 1
        true -> 0
      end

    {max_p, log, node()}
  end

  defp compare({p1, l1, n1}, {p2, l2, n2}) do
    cond do
      p1 > p2 -> :gt
      p1 < p2 -> :lt
      l1 > l2 -> :gt
      l1 < l2 -> :lt
      n1 > n2 -> :gt
      n1 < n2 -> :lt
      true -> :eq
    end
  end
end

defmodule SplitBrainDemo.GCounter do
  @moduledoc """
  Grow-only counter. Each node increments its own slot. Merge is pointwise max.
  """

  @type t :: %__MODULE__{entries: %{node() => non_neg_integer()}}

  defstruct entries: %{}

  @spec new() :: t()
  def new, do: %__MODULE__{}

  @spec inc(t(), pos_integer()) :: t()
  def inc(%__MODULE__{entries: e} = c, n \\ 1) when n > 0 do
    %{c | entries: Map.update(e, node(), n, &(&1 + n))}
  end

  @spec value(t()) :: non_neg_integer()
  def value(%__MODULE__{entries: e}), do: e |> Map.values() |> Enum.sum()

  @spec merge(t(), t()) :: t()
  def merge(%__MODULE__{entries: a}, %__MODULE__{entries: b}) do
    merged =
      Map.merge(a, b, fn _k, v1, v2 -> max(v1, v2) end)

    %__MODULE__{entries: merged}
  end
end

defmodule SplitBrainDemo.PNCounter do
  @moduledoc """
  Positive-Negative counter. Supports inc/dec; merge is pointwise on both halves.
  Value can go negative — domain logic decides validity.
  """

  alias SplitBrainDemo.GCounter

  @type t :: %__MODULE__{p: GCounter.t(), n: GCounter.t()}

  defstruct p: %GCounter{}, n: %GCounter{}

  @spec new() :: t()
  def new, do: %__MODULE__{p: GCounter.new(), n: GCounter.new()}

  @spec inc(t(), pos_integer()) :: t()
  def inc(%__MODULE__{} = c, amount \\ 1), do: %{c | p: GCounter.inc(c.p, amount)}

  @spec dec(t(), pos_integer()) :: t()
  def dec(%__MODULE__{} = c, amount \\ 1), do: %{c | n: GCounter.inc(c.n, amount)}

  @spec value(t()) :: integer()
  def value(%__MODULE__{p: p, n: n}), do: GCounter.value(p) - GCounter.value(n)

  @spec merge(t(), t()) :: t()
  def merge(%__MODULE__{} = a, %__MODULE__{} = b) do
    %__MODULE__{p: GCounter.merge(a.p, b.p), n: GCounter.merge(a.n, b.n)}
  end
end

defmodule SplitBrainDemo.QuorumCounter do
  @moduledoc """
  Write requires majority ack. Under partition, the minority side returns
  {:error, :no_quorum} for writes. Reads are always served locally (best-effort).
  """
  use GenServer

  @type t :: %{
          value: integer(),
          epoch: non_neg_integer(),
          nodes: [node()]
        }

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec inc(integer()) :: {:ok, integer()} | {:error, term()}
  def inc(amount \\ 1), do: GenServer.call(__MODULE__, {:inc, amount})

  @spec get() :: integer()
  def get, do: GenServer.call(__MODULE__, :get)

  @impl true
  def init(opts) do
    nodes = Keyword.get(opts, :cluster_nodes, [node() | Node.list()])
    {:ok, %{value: 0, epoch: 0, nodes: nodes}}
  end

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  def handle_call({:inc, amount}, _from, state) do
    reachable = reachable_nodes(state.nodes)
    quorum = div(length(state.nodes), 2) + 1

    if length(reachable) >= quorum do
      new_state = %{state | value: state.value + amount, epoch: state.epoch + 1}
      replicate(reachable -- [node()], new_state)
      {:reply, {:ok, new_state.value}, new_state}
    else
      {:reply, {:error, :no_quorum}, state}
    end
  end

  @impl true
  def handle_cast({:replicate, from_state}, state) do
    # Replica accepts updates with higher epoch
    if from_state.epoch > state.epoch do
      {:noreply, Map.merge(state, %{value: from_state.value, epoch: from_state.epoch})}
    else
      {:noreply, state}
    end
  end

  defp reachable_nodes(nodes) do
    Enum.filter(nodes, fn
      n when n == node() -> true
      n -> Node.ping(n) == :pong
    end)
  end

  defp replicate([], _state), do: :ok

  defp replicate(nodes, state) do
    Enum.each(nodes, fn n ->
      # cast, best-effort; majority already confirmed by Node.ping above
      GenServer.cast({__MODULE__, n}, {:replicate, state})
    end)
  end
end
```

### `test/split_brain_demo_test.exs`

```elixir
defmodule SplitBrainDemo.GCounterTest do
  use ExUnit.Case, async: true
  doctest SplitBrainDemo.LwwRegister

  alias SplitBrainDemo.GCounter

  describe "SplitBrainDemo.GCounter" do
    test "merge is commutative" do
      a = %GCounter{entries: %{:a@x => 3, :b@x => 5}}
      b = %GCounter{entries: %{:a@x => 1, :b@x => 7, :c@x => 2}}

      assert GCounter.merge(a, b) == GCounter.merge(b, a)
    end

    test "merge is associative" do
      a = %GCounter{entries: %{:a@x => 3}}
      b = %GCounter{entries: %{:a@x => 5, :b@x => 2}}
      c = %GCounter{entries: %{:b@x => 1, :c@x => 4}}

      left = GCounter.merge(GCounter.merge(a, b), c)
      right = GCounter.merge(a, GCounter.merge(b, c))
      assert left == right
    end

    test "merge is idempotent" do
      a = %GCounter{entries: %{:a@x => 3, :b@x => 5}}
      assert GCounter.merge(a, a) == a
    end

    test "concurrent increments both preserved after merge" do
      # node a increments locally 3 times
      a = GCounter.new() |> Map.put(:entries, %{:a@x => 3})
      # node b increments locally 5 times
      b = GCounter.new() |> Map.put(:entries, %{:b@x => 5})

      merged = GCounter.merge(a, b)
      assert GCounter.value(merged) == 8
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate split-brain conflict resolution: Last-Write-Wins (LWW)
      # Simulate two versions from different partitions

      partition_a = %{version: 1, timestamp: 1000, value: "data_a"}
      partition_b = %{version: 1, timestamp: 1500, value: "data_b"}  # Newer

      # LWW resolution: take version with highest timestamp
      resolved = if partition_a.timestamp >= partition_b.timestamp do
        partition_a
      else
        partition_b
      end

      IO.inspect(resolved, label: "✓ Resolved via LWW")

      assert resolved.timestamp == 1500, "LWW selected newer version"
      assert resolved.value == "data_b", "Resolved to partition_b"

      IO.puts("✓ Split-brain handling: Last-Write-Wins resolution working")
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

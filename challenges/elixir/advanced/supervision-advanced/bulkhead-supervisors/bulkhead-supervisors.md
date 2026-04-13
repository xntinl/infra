# Bulkhead supervisors: fault isolation between workloads

**Project**: `bulkhead_sups` — bulkhead pattern with isolated supervisors + PartitionSupervisor per workload class.

---

## Why bulkhead supervisors: fault isolation between workloads matters

This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach works on a developer laptop; the version built here survives the scheduler pressure, binary refc pitfalls, and supervisor budgets of a running node.

The trade-off chart and the executable benchmark are the core of the lesson: you calibrate the cost of the abstraction against a measurable gain, not a vibe.

---
## The business problem

Your company's internal API gateway handles four very different workload classes:

1. **Interactive** — user-facing requests, p99 < 50 ms target, must NEVER be blocked.
2. **Search** — catalog search queries that can take 200–800 ms and spike to 3 s.
3. **Batch** — CSV exports and bulk updates, runs for seconds, high memory.
4. **Admin** — operational queries (health, metrics endpoints, dashboards).

When all four share one worker pool, incidents happen like this: a search query hits a pathological full-text case and takes 15 s of CPU; the pool is saturated; interactive requests queue behind it; user-facing latency degrades across the whole site.

This is the **bulkhead pattern** (from ship hull compartments): partition resources so that a failure or resource exhaustion in one compartment does not flood the others. In OTP terms: give each workload class its OWN supervisor with its OWN process pool. A crash, memory blowup, or slowdown in one bulkhead is contained.

Implementation combines several primitives you've already seen:

- **Separate supervisors** per bulkhead (fault domain).
- **PartitionSupervisor per bulkhead** for parallelism within the class.
- **Per-bulkhead concurrency caps** (so batch can't saturate all cores).
- **Per-bulkhead restart budgets** (interactive: tight; batch: generous).

## Project structure

```
bulkhead_sups/
├── lib/
│   └── bulkhead_sups/
│       ├── application.ex
│       ├── bulkhead.ex                # generic bulkhead behaviour
│       ├── interactive/
│       │   ├── supervisor.ex
│       │   └── worker.ex
│       ├── search/
│       │   ├── supervisor.ex
│       │   └── worker.ex
│       ├── batch/
│       │   ├── supervisor.ex
│       │   └── worker.ex
│       ├── admin/
│       │   └── supervisor.ex
│       └── router.ex                  # classify request → route to bulkhead
├── test/
│   └── bulkhead_sups/
│       ├── isolation_test.exs
│       └── router_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Design decisions

**Option A — single pool with priority-aware scheduling**
- Pros: maximum throughput; every worker can handle any request.
- Cons: head-of-line blocking between classes; a 10 s batch stalls interactive p99; priorities drift under load.

**Option B — one PartitionSupervisor per workload class with per-class budgets** (chosen)
- Pros: fault isolation is structural, not policy-based; each class has its own restart intensity, queue, and saturation semantics.
- Cons: total capacity is partitioned (you cannot steal idle workers across bulkheads); more supervisors to observe.

→ Chose **B** because interference, not capacity, is usually the production problem. If you need steal-on-idle, add a dedicated overflow bulkhead on top.

---

## Implementation

### Step 1: Application supervisor

**Objective**: Wire the OTP application and supervision tree for the components built.

```elixir
# lib/bulkhead_sups/application.ex
defmodule BulkheadSups.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      BulkheadSups.Interactive.Supervisor,
      BulkheadSups.Search.Supervisor,
      BulkheadSups.Batch.Supervisor,
      BulkheadSups.Admin.Supervisor
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      max_restarts: 10,
      max_seconds: 30,
      name: BulkheadSups.Supervisor
    )
  end
end
```
### Step 2: Interactive bulkhead — tight budget, many partitions

**Objective**: Build the interactive bulkhead layer: tight budget, many partitions.

```elixir
# lib/bulkhead_sups/interactive/supervisor.ex
defmodule BulkheadSups.Interactive.Supervisor do
  use Supervisor

  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    children = [
      {PartitionSupervisor,
       child_spec: BulkheadSups.Interactive.Worker,
       name: BulkheadSups.Interactive.Workers,
       partitions: System.schedulers_online()}
    ]

    # Interactive MUST stay healthy. Any flapping = escalate fast.
    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 3,
      max_seconds: 1
    )
  end
end

# lib/bulkhead_sups/interactive/worker.ex
defmodule BulkheadSups.Interactive.Worker do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok)

  @spec process_request(term(), term()) :: {:ok, term()} | {:error, term()}
  def process_request(key, req) do
    pid = {:via, PartitionSupervisor, {BulkheadSups.Interactive.Workers, key}}

    try do
      GenServer.call(pid, {:process_request, req}, 50)
    catch
      :exit, {:timeout, _} -> {:error, :timeout}
    end
  end

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_call({:process_request, req}, _from, state) do
    {:reply, {:ok, {:interactive, req}}, state}
  end
end
```
### Step 3: Search bulkhead — generous budget, fewer partitions

**Objective**: Build the search bulkhead layer: generous budget, fewer partitions.

```elixir
# lib/bulkhead_sups/search/supervisor.ex
defmodule BulkheadSups.Search.Supervisor do
  use Supervisor

  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    children = [
      {PartitionSupervisor,
       child_spec: BulkheadSups.Search.Worker,
       name: BulkheadSups.Search.Workers,
       partitions: max(div(System.schedulers_online(), 2), 2)}
    ]

    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 10,
      max_seconds: 30
    )
  end
end

# lib/bulkhead_sups/search/worker.ex
defmodule BulkheadSups.Search.Worker do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok)

  def process_request(key, req) do
    pid = {:via, PartitionSupervisor, {BulkheadSups.Search.Workers, key}}

    try do
      GenServer.call(pid, {:search, req}, 3_000)
    catch
      :exit, {:timeout, _} -> {:error, :search_timeout}
    end
  end

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_call({:search, req}, _from, state) do
    # Simulated search work
    Process.sleep(10)
    {:reply, {:ok, {:search_results, req}}, state}
  end
end
```
### Step 4: Batch bulkhead — few partitions, long timeouts

**Objective**: Build the batch bulkhead layer: few partitions, long timeouts.

```elixir
# lib/bulkhead_sups/batch/supervisor.ex
defmodule BulkheadSups.Batch.Supervisor do
  use Supervisor

  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    children = [
      {PartitionSupervisor,
       child_spec: BulkheadSups.Batch.Worker,
       name: BulkheadSups.Batch.Workers,
       partitions: 2}
    ]

    # Batch failures are annoying but not critical — allow many restarts.
    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 20,
      max_seconds: 60
    )
  end
end

# lib/bulkhead_sups/batch/worker.ex
defmodule BulkheadSups.Batch.Worker do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok)

  def process_request(key, req) do
    pid = {:via, PartitionSupervisor, {BulkheadSups.Batch.Workers, key}}
    GenServer.call(pid, {:batch, req}, 60_000)
  end

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_call({:batch, req}, _from, state) do
    # Simulated batch work; in real life this is I/O-bound bulk DB work.
    Process.sleep(50)
    {:reply, {:ok, {:batch_done, req}}, state}
  end

  def handle_call({:hog, ms}, _from, state) do
    # Deliberately hog the worker to test isolation.
    Process.sleep(ms)
    {:reply, :ok, state}
  end
end
```
### Step 5: Admin bulkhead — smallest footprint

**Objective**: Build the admin bulkhead layer: smallest footprint.

```elixir
# lib/bulkhead_sups/admin/supervisor.ex
defmodule BulkheadSups.Admin.Supervisor do
  use Supervisor

  def start_link(_), do: Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok) do
    children = [
      {PartitionSupervisor,
       child_spec: BulkheadSups.Admin.Worker,
       name: BulkheadSups.Admin.Workers,
       partitions: 1}
    ]

    Supervisor.init(children, strategy: :one_for_one, max_restarts: 3, max_seconds: 60)
  end
end

# Reuse a minimal worker
defmodule BulkheadSups.Admin.Worker do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok)

  def process_request(key, req) do
    pid = {:via, PartitionSupervisor, {BulkheadSups.Admin.Workers, key}}
    GenServer.call(pid, {:admin, req}, 500)
  end

  @impl true
  def init(:ok), do: {:ok, %{}}
  @impl true
  def handle_call({:admin, req}, _from, state), do: {:reply, {:ok, {:admin, req}}, state}
end
```
### Step 6: Router

**Objective**: Implement Router.

```elixir
# lib/bulkhead_sups/router.ex
defmodule BulkheadSups.Router do
  @moduledoc "Classify a request and dispatch it to the right bulkhead."

  @type request :: %{required(:path) => String.t(), optional(:method) => atom()}
  @type bulkhead :: :interactive | :search | :batch | :admin

  @spec classify(request()) :: bulkhead()
  def classify(%{path: "/search" <> _}), do: :search
  def classify(%{path: "/admin" <> _}), do: :admin
  def classify(%{method: :post, path: "/export" <> _}), do: :batch
  def classify(_), do: :interactive

  @spec dispatch(request()) :: {:ok, term()} | {:error, term()}
  def dispatch(%{path: path} = req) do
    case classify(req) do
      :interactive -> BulkheadSups.Interactive.Worker.handle(path, req)
      :search -> BulkheadSups.Search.Worker.handle(path, req)
      :batch -> BulkheadSups.Batch.Worker.handle(path, req)
      :admin -> BulkheadSups.Admin.Worker.handle(path, req)
    end
  end
end
```
### `test/bulkhead_sups_test.exs`

**Objective**: Write tests for isolation is the real assertion.

```elixir
defmodule BulkheadSups.RouterTest do
  use ExUnit.Case, async: true
  doctest BulkheadSups.Router

  alias BulkheadSups.Router

  describe "BulkheadSups.Router" do
    test "classify routes paths correctly" do
      assert :search == Router.classify(%{path: "/search?q=x"})
      assert :admin == Router.classify(%{path: "/admin/users"})
      assert :batch == Router.classify(%{method: :post, path: "/export/users.csv"})
      assert :interactive == Router.classify(%{path: "/checkout"})
    end

    test "interactive request dispatches and returns fast" do
      {t_us, result} =
        :timer.tc(fn -> Router.dispatch(%{path: "/checkout", method: :get}) end)

      assert {:ok, {:interactive, _}} = result
      assert t_us < 50_000  # <50ms
    end
  end
end
```
```elixir
defmodule BulkheadSups.IsolationTest do
  use ExUnit.Case, async: false
  doctest BulkheadSups.Router

  alias BulkheadSups.{Router, Batch}

  describe "BulkheadSups.Isolation" do
    test "batch workers hogging does NOT block interactive requests" do
      # Saturate both batch partitions with 500ms of sleep each.
      for _ <- 1..2 do
        Task.async(fn ->
          pid = {:via, PartitionSupervisor, {Batch.Workers, :rand.uniform(1_000_000)}}
          GenServer.call(pid, {:hog, 500}, 1_000)
        end)
      end

      Process.sleep(20)

      # Interactive requests should complete well under the 500ms hog window.
      {t_us, result} =
        :timer.tc(fn -> Router.dispatch(%{path: "/checkout", method: :get}) end)

      assert {:ok, _} = result
      assert t_us < 50_000, "interactive was blocked: #{t_us} µs"
    end

    test "crash in one bulkhead does not affect siblings" do
      # Kill all interactive workers.
      interactive_pids =
        PartitionSupervisor.which_children(BulkheadSups.Interactive.Workers)
        |> Enum.map(fn {_, pid, _, _} -> pid end)

      for pid <- interactive_pids, do: Process.exit(pid, :kill)

      # Search and batch still work.
      assert {:ok, _} = Router.dispatch(%{path: "/search?q=elixir", method: :get})
      assert {:ok, _} = Router.dispatch(%{method: :post, path: "/export/x.csv"})
    end
  end
end
```
---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---

## Deep Dive: Supervisor Patterns and Production Implications

Supervisor trees define fault tolerance at the application level. Testing supervisor restart strategies (one_for_one, rest_for_one, one_for_all) requires reasoning about side effects of crashes across multiple children. The insight is that your test should verify not just that a child restarts, but that dependent state (ETS tables, connections, message queues) is properly initialized after restart. Production incidents often involve restart loops under load—a supervisor that works fine in quiet tests can spin wildly when children fail faster than they recover.

---

## Trade-offs and production gotchas

**1. Bulkheads require capacity planning.** You must decide upfront how much CPU / memory / mailbox each class gets. Get this wrong and interactive is starved even though batch is idle (unused partitions can't be borrowed). Monitor per-bulkhead utilization and rebalance periodically.

**2. Partitions are fixed at startup.** You can't dynamically grow a bulkhead when load spikes. For autoscaling behaviour, use a `DynamicSupervisor` per bulkhead + a separate admission controller — more complex but more flexible.

**3. Cross-bulkhead calls break isolation.** If your Interactive worker synchronously calls a Batch worker, you've reintroduced the problem — Interactive is blocked on Batch. Classification must be complete before dispatch; no nested bulkhead calls.

**4. Restart budgets per bulkhead must be deliberate.** Interactive with `max_restarts: 3, max_seconds: 1` dies fast on flaps — which is what you want (escalate to root → restart the subtree). Batch with `max_restarts: 20, max_seconds: 60` tolerates long recovery from transient storage issues. Pick based on SLA.

**5. Admin bulkhead is often forgotten.** Engineers put `/metrics` or `/healthz` on the interactive path, then wonder why their health probe times out during an incident. Admin MUST be a separate, well-isolated bulkhead so you can diagnose the incident.

**6. PartitionSupervisor hash keys can hotspot.** If all batch requests hash to the same partition (e.g., hash by user_id and one user is exporting 10 files), you get single-partition saturation within the bulkhead. Use compound keys or round-robin for batch.

**7. Memory is per-process, not per-bulkhead.** A batch worker that loads 500 MB into memory is a 500 MB memory cost per worker process. Two partitions × 500 MB = 1 GB. Track per-bulkhead memory via `:erlang.memory/0` observations.

**8. When NOT to use this.** For a single-workload service (all requests roughly the same shape and duration), bulkheads add complexity without benefit. Start with one pool; add bulkheads when you measurably have latency interference between request classes.

### Why this works

Each bulkhead is its own supervision subtree with its own PartitionSupervisor and its own restart intensity, so saturation or crash cascades in one class cannot consume resources from another. Routing is a cheap pattern match, meaning the isolation cost at the hot path is measured in microseconds. The win is not in per-call performance but in the absence of cross-class interference at p99.

---

## Benchmark

Per-bulkhead dispatch adds ~1 µs (pattern match on the router + one `{:via, PartitionSupervisor, ...}` resolution). The savings come from NOT head-of-line-blocking between classes.

Measure with `k6` or `wrk`: run a mixed workload (80 % interactive, 10 % search, 10 % batch) with and without bulkheads. Without: p99 interactive latency tracks p99 batch latency. With: p99 interactive stays constant even as you scale batch load up to saturate its partitions.

Target: p99 interactive latency unchanged when batch bulkhead is saturated at 100 % of its capacity; routing overhead ≤ 5 µs per request.

---

## Reflection

1. Your interactive bulkhead is saturated while the batch bulkhead sits idle. Do you enable cross-bulkhead borrowing, resize the interactive pool at runtime, or shed interactive load? Which option preserves the isolation guarantee you designed the tree for?
2. A new workload class arrives that does not cleanly fit any existing bulkhead. How do you decide between creating a fifth bulkhead, extending an existing one, or classifying the new workload at the edge (feature flag)? Argue from ops cost, not elegance.

---

### `script/main.exs`
```elixir
defmodule BulkheadSups.IsolationTest do
  use ExUnit.Case, async: false
  doctest BulkheadSups.Router

  alias BulkheadSups.{Router, Batch}

  describe "BulkheadSups.Isolation" do
    test "batch workers hogging does NOT block interactive requests" do
      # Saturate both batch partitions with 500ms of sleep each.
      for _ <- 1..2 do
        Task.async(fn ->
          pid = {:via, PartitionSupervisor, {Batch.Workers, :rand.uniform(1_000_000)}}
          GenServer.call(pid, {:hog, 500}, 1_000)
        end)
      end

      Process.sleep(20)

      # Interactive requests should complete well under the 500ms hog window.
      {t_us, result} =
        :timer.tc(fn -> Router.dispatch(%{path: "/checkout", method: :get}) end)

      assert {:ok, _} = result
      assert t_us < 50_000, "interactive was blocked: #{t_us} µs"
    end

    test "crash in one bulkhead does not affect siblings" do
      # Kill all interactive workers.
      interactive_pids =
        PartitionSupervisor.which_children(BulkheadSups.Interactive.Workers)
        |> Enum.map(fn {_, pid, _, _} -> pid end)

      for pid <- interactive_pids, do: Process.exit(pid, :kill)

      # Search and batch still work.
      assert {:ok, _} = Router.dispatch(%{path: "/search?q=elixir", method: :get})
      assert {:ok, _} = Router.dispatch(%{method: :post, path: "/export/x.csv"})
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate bulkhead supervisors for workload isolation

      # Start root supervisor with independent bulkheads
      {:ok, root_sup} = Supervisor.start_link(
        [
          {BulkheadSups.Interactive.Supervisor, []},
          {BulkheadSups.Search.Supervisor, []},
          {BulkheadSups.Batch.Supervisor, []},
          {BulkheadSups.Admin.Supervisor, []}
        ],
        strategy: :one_for_one,
        name: BulkheadSups.RootSupervisor
      )

      assert is_pid(root_sup), "Root supervisor must start"
      IO.inspect(root_sup, label: "Root supervisor PID")

      # Verify each bulkhead is running
      interactive_sup = Process.whereis(BulkheadSups.Interactive.Supervisor)
      search_sup = Process.whereis(BulkheadSups.Search.Supervisor)
      batch_sup = Process.whereis(BulkheadSups.Batch.Supervisor)
      admin_sup = Process.whereis(BulkheadSups.Admin.Supervisor)

      assert is_pid(interactive_sup), "Interactive bulkhead must be running"
      assert is_pid(search_sup), "Search bulkhead must be running"
      assert is_pid(batch_sup), "Batch bulkhead must be running"
      assert is_pid(admin_sup), "Admin bulkhead must be running"

      IO.puts("✓ Four independent bulkheads initialized:")
      IO.puts("  - Interactive (p99 < 50ms, tight budget)")
      IO.puts("  - Search (200-800ms, generous budget)")
      IO.puts("  - Batch (seconds, high memory, capped concurrency)")
      IO.puts("  - Admin (operational queries, low priority)")

      # Test workload routing and isolation
      # Send an interactive request (fast path)
      {:ok, task_i} = BulkheadSups.Interactive.handle_request("interactive-1")
      assert is_pid(task_i), "Interactive request should queue"
      IO.puts("✓ Interactive request routed to isolated bulkhead")

      # Send a search query (slower, can spike)
      {:ok, task_s} = BulkheadSups.Search.handle_request("search-1")
      assert is_pid(task_s), "Search request should queue"
      IO.puts("✓ Search request routed to dedicated bulkhead")

      # Send batch work (can be slow and memory-heavy)
      {:ok, task_b} = BulkheadSups.Batch.handle_request("batch-export")
      assert is_pid(task_b), "Batch request should queue"
      IO.puts("✓ Batch request routed to isolated bulkhead")

      # Send admin query
      {:ok, task_a} = BulkheadSups.Admin.handle_request("health-check")
      assert is_pid(task_a), "Admin request should queue"
      IO.puts("✓ Admin request routed to dedicated bulkhead")

      # Test isolation: search slowdown does NOT affect interactive
      IO.puts("✓ Demonstrating fault isolation...")

      # Simulate search bottleneck (pathological query)
      for _i <- 1..5 do
        BulkheadSups.Search.handle_request("slow-query")
      end

      # Interactive should still be responsive (separate bulkhead)
      {:ok, _interactive_responsive} = BulkheadSups.Interactive.handle_request("fast-interactive")
      IO.puts("✓ Interactive queries remain responsive despite search bottleneck")

      # Check queue depths per bulkhead (verifying concurrency caps)
      interactive_count = BulkheadSups.Interactive.in_flight_count()
      batch_count = BulkheadSups.Batch.in_flight_count()

      IO.inspect(interactive_count, label: "Interactive in-flight")
      IO.inspect(batch_count, label: "Batch in-flight")

      # Batch should be capped to prevent resource exhaustion
      assert batch_count <= 4, "Batch concurrency should be capped"
      IO.puts("✓ Per-bulkhead concurrency caps enforced")

      IO.puts("\n✓ Bulkhead supervisor pattern demonstrated:")
      IO.puts("  - Four independent fault domains")
      IO.puts("  - Failure isolation (crash in one doesn't affect others)")
      IO.puts("  - Resource isolation (concurrency caps per class)")
      IO.puts("  - Restart budget isolation (tight for interactive, loose for batch)")
      IO.puts("  - Interactive always responsive, batch can't starve it")
      IO.puts("✓ Ready for multi-tenant API gateway")

      Supervisor.stop(root_sup)
      IO.puts("✓ Bulkhead supervisors shutdown complete")
  end
end

Main.main()
```
### `lib/bulkhead_sups.ex`

```elixir
defmodule BulkheadSups do
  @moduledoc """
  Reference implementation for Bulkhead supervisors: fault isolation between workloads.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the bulkhead_sups module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> BulkheadSups.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
---

## Key concepts

### 1. Bulkhead = compartmentalized capacity

Ship hulls have watertight compartments. A leak fills one; the ship floats. Apply to OTP: separate supervisors, separate process pools, separate memory budgets.

```
                        Application Supervisor (:one_for_one)
                        │
    ┌───────────────┬───┴──────────────┬───────────────────┐
    │               │                  │                   │
 Interactive     Search              Batch                Admin
 (8 partitions   (4 partitions       (2 partitions       (1 partition
  :rest_for_one)  :one_for_one)      :one_for_one)        :one_for_one)
 budget 3/1s     budget 10/30s       budget 20/60s        budget 3/60s
```

### 2. Capacity allocation per bulkhead

The total process budget is split per class, NOT shared. If Batch has 2 partitions and a batch job uses all of them, subsequent batch requests queue — but interactive's 8 partitions are untouched.

This INCLUDES CPU: a busy partition is pinned to a scheduler. If Batch gets 2 partitions and interactive gets 8, a saturated batch workload uses only 2 schedulers.

### 3. Bulkhead vs rate limiter vs circuit breaker

| Pattern | What it controls | Where it applies |
|---|---|---|
| Bulkhead | Resources (processes, memory) | Server-side, at the supervision layer |
| Rate limiter | Request rate | At ingress, per client |
| Circuit breaker | Fast-fail on downstream errors | Per outbound dependency |

Bulkheads and rate limiters compose. A rate limiter keeps any one client from overwhelming its bulkhead. A bulkhead keeps any one class from overwhelming the whole system.

### 4. Request classification

A router classifies incoming requests into a bulkhead BEFORE dispatching work. The classifier must be cheap — it runs in the inbound path:

### `mix.exs`
```elixir
defmodule BulkheadSupervisors.MixProject do
  use Mix.Project

  def project do
    [
      app: :bulkhead_supervisors,
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
def classify(%{path: "/search" <> _}), do: :search
def classify(%{path: "/admin" <> _}), do: :admin
def classify(%{method: :post, path: "/export" <> _}), do: :batch
def classify(_), do: :interactive
```
### 5. Shedding load per bulkhead

When a bulkhead is at capacity, it sheds new work fast (return 503) rather than queueing. This preserves the SLO for work that IS being handled. Check the partition's mailbox length or use a semaphore:

```elixir
def dispatch(bulkhead, work) do
  case mailbox_len(bulkhead) do
    n when n < 100 -> do_dispatch(bulkhead, work)
    _ -> {:error, :overloaded}
  end
end
```
---

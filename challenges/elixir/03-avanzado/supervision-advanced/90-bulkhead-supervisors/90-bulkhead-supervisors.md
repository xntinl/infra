# Bulkhead supervisors: fault isolation between workloads

**Project**: `bulkhead_sups` — bulkhead pattern with isolated supervisors + PartitionSupervisor per workload class.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

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
└── test/
    └── bulkhead_sups/
        ├── isolation_test.exs
        └── router_test.exs
```

---

## Core concepts

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

## Implementation

### Step 1: Application supervisor

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

  @spec handle(term(), term()) :: {:ok, term()} | {:error, term()}
  def handle(key, req) do
    pid = {:via, PartitionSupervisor, {BulkheadSups.Interactive.Workers, key}}

    try do
      GenServer.call(pid, {:handle, req}, 50)
    catch
      :exit, {:timeout, _} -> {:error, :timeout}
    end
  end

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_call({:handle, req}, _from, state) do
    {:reply, {:ok, {:interactive, req}}, state}
  end
end
```

### Step 3: Search bulkhead — generous budget, fewer partitions

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

  def handle(key, req) do
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

  def handle(key, req) do
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

  def handle(key, req) do
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

### Step 7: Tests — isolation is the real assertion

```elixir
# test/bulkhead_sups/router_test.exs
defmodule BulkheadSups.RouterTest do
  use ExUnit.Case, async: true

  alias BulkheadSups.Router

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
```

```elixir
# test/bulkhead_sups/isolation_test.exs
defmodule BulkheadSups.IsolationTest do
  use ExUnit.Case, async: false

  alias BulkheadSups.{Router, Batch}

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
```

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

---

## Performance notes

Per-bulkhead dispatch adds ~1 µs (pattern match on the router + one `{:via, PartitionSupervisor, ...}` resolution). The savings come from NOT head-of-line-blocking between classes.

Measure with `k6` or `wrk`: run a mixed workload (80 % interactive, 10 % search, 10 % batch) with and without bulkheads. Without: p99 interactive latency tracks p99 batch latency. With: p99 interactive stays constant even as you scale batch load up to saturate its partitions.

---

## Resources

- [Michael Nygard — *Release It!*](https://pragprog.com/titles/mnee2/release-it-second-edition/) — original "Bulkheads" chapter; foundational reading.
- [Resilience4j — Bulkhead pattern](https://resilience4j.readme.io/docs/bulkhead) — Java analogue; similar reasoning.
- [Netflix Hystrix — thread pool isolation](https://github.com/Netflix/Hystrix/wiki/How-it-Works#ThreadPoolIsolation) — the classic cloud-era bulkhead.
- [`PartitionSupervisor`](https://hexdocs.pm/elixir/PartitionSupervisor.html) — the BEAM primitive used here.
- [Fred Hébert — Handling Overload](https://ferd.ca/handling-overload.html) — bulkheads + load shedding together.
- [Finch connection pools — per-host isolation](https://github.com/sneako/finch) — real-world bulkhead pattern for HTTP outbound traffic.
- [Broadway concurrency — per-processor supervisors](https://github.com/dashbitco/broadway) — bulkheads between pipeline stages.

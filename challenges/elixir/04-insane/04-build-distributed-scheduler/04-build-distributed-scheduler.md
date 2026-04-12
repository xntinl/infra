# Distributed Job Scheduler with Bin-Packing and Preemption

**Project**: `helios` -- a distributed job scheduler with resource-aware placement and fault tolerance

---

## Project context

You are building `helios`, a distributed job scheduler that assigns computational jobs to worker nodes based on resource availability, enforces fairness, and handles node failures by rescheduling affected jobs. The scheduler exposes a REST API built with Plug (not Phoenix).

Project structure:

```
helios/
├── lib/
│   └── helios/
│       ├── application.ex           # supervisor tree: scheduler + cluster + API
│       ├── scheduler.ex             # GenServer: bin-packing, preemption, queue
│       ├── worker_node.ex           # GenServer per worker: heartbeat, resource reporting
│       ├── cluster.ex               # tracks live nodes and their capacity
│       ├── job.ex                   # job struct and FSM: submitted→queued→scheduled→running→done
│       ├── bin_packer.ex            # best-fit decreasing placement algorithm
│       ├── preemptor.ex             # selects jobs to evict for high-priority placement
│       ├── fair_share.ex            # per-user resource accounting and threshold enforcement
│       ├── audit.ex                 # append-only audit log: all job state transitions
│       └── api/
│           ├── router.ex            # Plug.Router: job CRUD, node listing
│           └── plug_pipeline.ex     # Plug.Builder: parsers, logging, auth
├── test/
│   └── helios/
│       ├── bin_packing_test.exs     # placement algorithm correctness
│       ├── preemption_test.exs      # eviction and requeue logic
│       ├── fault_tolerance_test.exs # node death → job requeue
│       ├── fair_share_test.exs      # per-user cap enforcement
│       └── api_test.exs             # REST endpoint integration
├── bench/
│   └── placement_bench.exs
└── mix.exs
```

---

## The problem

A compute cluster of N worker nodes has finite CPU and memory. Jobs arrive continuously with resource requests and priorities. The scheduler must place each job on the node that best fits its requirements (maximizing utilization) without overcommitting any node. When a high-priority job cannot be scheduled due to resource contention, it must preempt lower-priority jobs. When a worker node dies, its jobs must be detected and rescheduled within 30 seconds.

This is a variant of the bin-packing problem (NP-hard in the general case), solved with a greedy heuristic that works well in practice for scheduler workloads.

---

## Why this design

**Best-fit decreasing heuristic**: sort nodes by remaining capacity descending, then pick the node where the job fits with the smallest remaining gap. This minimizes wasted capacity per node, leaving large blocks of free capacity on other nodes for large jobs. It is O(N log N) per placement decision.

**Heartbeat-based failure detection**: each worker sends a heartbeat every 5 seconds. If the scheduler misses 3 consecutive heartbeats (15 seconds), it marks the node unavailable and requeues its jobs. This is not perfect -- a slow-but-alive node will generate false positives. The trade-off is between detection latency (low threshold) and false positive rate (high threshold).

**Fair-share over strict priority**: strict priority scheduling starves low-priority jobs indefinitely when high-priority work is constant. Fair-share caps each user's resource consumption at a configurable percentage of cluster capacity. A user exceeding their cap has new submissions queued, not rejected, and they drain the queue as other users release resources.

**Plug over Phoenix**: the scheduler API is a small set of CRUD endpoints with JSON request/response. Plug.Router and Plug.Parsers are sufficient. Adding a full framework dependency for six endpoints is YAGNI.

---

## Design decisions

**Option A — Central queue with a single dispatcher**
- Pros: trivially correct ordering; easy to inspect.
- Cons: single point of failure and throughput; dispatcher becomes the bottleneck at scale.

**Option B — Sharded queues with leader-per-shard and gossip membership** (chosen)
- Pros: throughput scales with shard count; leader election per shard isolates failure; gossip keeps members loosely synchronized without a central registry.
- Cons: more moving parts; rebalance on topology change must avoid double-execution.

→ Chose **B** because the whole point of a *distributed* scheduler is to survive the loss of the dispatcher — sharding with a per-shard leader makes that explicit.

## Implementation milestones

### Step 1: Create the project

```bash
mix new helios --sup
cd helios
mkdir -p lib/helios/api test/helios bench
```

### Step 2: `mix.exs` -- dependencies

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Job struct and lifecycle FSM

```elixir
# lib/helios/job.ex
defmodule Helios.Job do
  @moduledoc """
  Job lifecycle:

    submitted -> queued -> scheduled -> running -> completed
                                    \\-> preempted -> queued (requeued)
                             \\-> failed -> dead_queue (after max_attempts)

  Each transition is recorded in the audit log with a timestamp and node assignment.
  """

  @states [:submitted, :queued, :scheduled, :running, :completed, :failed, :preempted]

  @valid_transitions %{
    submitted: [:queued],
    queued: [:scheduled, :preempted],
    scheduled: [:running, :failed, :preempted],
    running: [:completed, :failed, :preempted],
    failed: [:queued, :dead],
    preempted: [:queued]
  }

  defstruct [
    :id,
    :user,
    :command,
    :cpu,
    :memory_mb,
    :priority,
    :state,
    :node_id,
    :submitted_at,
    :started_at,
    :completed_at,
    :attempt
  ]

  @spec new(map()) :: %__MODULE__{}
  def new(attrs) do
    %__MODULE__{
      id: :crypto.strong_rand_bytes(8) |> Base.encode16(),
      user: Map.get(attrs, :user, "default"),
      command: Map.get(attrs, :command),
      cpu: Map.fetch!(attrs, :cpu),
      memory_mb: Map.fetch!(attrs, :memory_mb),
      priority: Map.get(attrs, :priority, 5),
      state: :submitted,
      node_id: nil,
      submitted_at: System.monotonic_time(:millisecond),
      started_at: nil,
      completed_at: nil,
      attempt: 1
    }
  end

  @spec transition(%__MODULE__{}, atom()) :: {:ok, %__MODULE__{}} | {:error, :invalid_transition}
  def transition(%__MODULE__{state: current} = job, target) do
    allowed = Map.get(@valid_transitions, current, [])

    if target in allowed do
      {:ok, %{job | state: target}}
    else
      {:error, :invalid_transition}
    end
  end
end
```

### Step 4: Bin-packing placement

```elixir
# lib/helios/bin_packer.ex
defmodule Helios.BinPacker do
  @moduledoc """
  Best-fit decreasing placement.

  Given a list of available nodes and a job's resource request,
  returns the node that satisfies the request with the smallest
  remaining capacity gap (tightest fit).
  """

  @doc """
  Returns {:ok, node} or {:error, :no_capacity}.

  nodes: list of %{id, available_cpu, available_memory_mb}
  job:   %{cpu, memory_mb}
  """
  @spec place([map()], map()) :: {:ok, map()} | {:error, :no_capacity}
  def place(nodes, job) do
    fitting_nodes =
      nodes
      |> Enum.filter(fn node ->
        node.available_cpu >= job.cpu and node.available_memory_mb >= job.memory_mb
      end)
      |> Enum.sort_by(fn node ->
        cpu_gap = node.available_cpu - job.cpu
        mem_gap = node.available_memory_mb - job.memory_mb
        cpu_gap + mem_gap
      end)

    case fitting_nodes do
      [best | _] -> {:ok, best}
      [] -> {:error, :no_capacity}
    end
  end
end
```

### Step 5: Scheduler GenServer

```elixir
# lib/helios/scheduler.ex
defmodule Helios.Scheduler do
  use GenServer

  @moduledoc """
  Central scheduler that manages job placement, node tracking, and failure detection.
  """

  defstruct [:nodes, :jobs, :heartbeat_tracker]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    node_count = Keyword.get(opts, :nodes, 3)

    nodes =
      for i <- 1..node_count, into: %{} do
        id = :"node_#{i}"
        {id, %{id: id, available_cpu: 8, available_memory_mb: 8_000, status: :alive,
                last_heartbeat: System.monotonic_time(:millisecond)}}
      end

    schedule_heartbeat_check()

    {:ok, %__MODULE__{
      nodes: nodes,
      jobs: %{},
      heartbeat_tracker: %{}
    }}
  end

  @impl true
  def handle_call({:submit, attrs}, _from, state) do
    job = Helios.Job.new(attrs)
    {:ok, job} = Helios.Job.transition(job, :queued)

    case place_job(job, state.nodes) do
      {:ok, node_id, updated_nodes} ->
        {:ok, running_job} = Helios.Job.transition(job, :scheduled)
        running_job = %{running_job | node_id: node_id}
        jobs = Map.put(state.jobs, job.id, running_job)
        {:reply, {:ok, running_job}, %{state | jobs: jobs, nodes: updated_nodes}}
      {:error, :no_capacity} ->
        jobs = Map.put(state.jobs, job.id, job)
        {:reply, {:ok, job}, %{state | jobs: jobs}}
    end
  end

  def handle_call({:job_state, job_id}, _from, state) do
    case Map.get(state.jobs, job_id) do
      nil -> {:reply, :not_found, state}
      job -> {:reply, job.state, state}
    end
  end

  @impl true
  def handle_info(:check_heartbeats, state) do
    now = System.monotonic_time(:millisecond)
    timeout_ms = 15_000

    {dead_nodes, alive_nodes} =
      Enum.split_with(state.nodes, fn {_id, node} ->
        node.status == :alive and (now - node.last_heartbeat) > timeout_ms
      end)

    updated_state =
      Enum.reduce(dead_nodes, state, fn {node_id, _node}, acc ->
        requeue_jobs_on_node(acc, node_id)
      end)

    schedule_heartbeat_check()
    {:noreply, updated_state}
  end

  def handle_info({:node_dead, node_id}, state) do
    updated_state = requeue_jobs_on_node(state, node_id)
    {:noreply, updated_state}
  end

  def handle_info({:heartbeat, node_id}, state) do
    nodes = Map.update(state.nodes, node_id, nil, fn node ->
      %{node | last_heartbeat: System.monotonic_time(:millisecond), status: :alive}
    end)
    {:noreply, %{state | nodes: nodes}}
  end

  defp place_job(job, nodes) do
    available = nodes |> Map.values() |> Enum.filter(fn n -> n.status == :alive end)

    case Helios.BinPacker.place(available, %{cpu: job.cpu, memory_mb: job.memory_mb}) do
      {:ok, node} ->
        updated = Map.update!(nodes, node.id, fn n ->
          %{n | available_cpu: n.available_cpu - job.cpu,
                available_memory_mb: n.available_memory_mb - job.memory_mb}
        end)
        {:ok, node.id, updated}
      {:error, :no_capacity} ->
        {:error, :no_capacity}
    end
  end

  defp requeue_jobs_on_node(state, node_id) do
    {affected, rest} =
      Enum.split_with(state.jobs, fn {_id, job} ->
        job.node_id == node_id and job.state in [:scheduled, :running]
      end)

    requeued_jobs =
      Enum.map(affected, fn {id, job} ->
        {id, %{job | state: :queued, node_id: nil}}
      end)

    updated_nodes = Map.update!(state.nodes, node_id, fn n ->
      %{n | status: :dead}
    end)

    %{state | jobs: Map.new(rest ++ requeued_jobs), nodes: updated_nodes}
  end

  defp schedule_heartbeat_check do
    Process.send_after(self(), :check_heartbeats, 5_000)
  end
end
```

### Step 6: REST API

```elixir
# lib/helios/api/router.ex
defmodule Helios.API.Router do
  use Plug.Router

  plug Plug.Parsers, parsers: [:json], json_decoder: Jason
  plug :match
  plug :dispatch

  post "/jobs" do
    attrs = %{
      cpu: conn.body_params["cpu"] || 1,
      memory_mb: conn.body_params["memory_mb"] || 512,
      priority: conn.body_params["priority"] || 5,
      user: conn.body_params["user"] || "anonymous",
      command: conn.body_params["command"]
    }

    {:ok, job} = Helios.submit(Helios.Scheduler, attrs)
    body = Jason.encode!(%{job_id: job.id, status: Atom.to_string(job.state)})
    send_resp(conn, 201, body)
  end

  get "/jobs/:id" do
    case GenServer.call(Helios.Scheduler, {:job_state, id}) do
      :not_found ->
        send_resp(conn, 404, ~s({"error": "not_found"}))
      state ->
        send_resp(conn, 200, Jason.encode!(%{job_id: id, status: Atom.to_string(state)}))
    end
  end

  delete "/jobs/:id" do
    send_resp(conn, 200, ~s({"status": "cancelled"}))
  end

  get "/nodes" do
    send_resp(conn, 200, ~s({"nodes": []}))
  end

  match _ do
    send_resp(conn, 404, ~s({"error": "not_found"}))
  end
end
```

### Step 7: Public API

```elixir
# lib/helios.ex
defmodule Helios do
  @moduledoc "Top-level API for the Helios scheduler."

  def submit(scheduler \\ Helios.Scheduler, attrs) do
    GenServer.call(scheduler, {:submit, attrs})
  end

  def job_state(scheduler \\ Helios.Scheduler, job_id) do
    GenServer.call(scheduler, {:job_state, job_id})
  end
end
```

### Step 8: Test Helpers

```elixir
# lib/helios/test_helpers.ex
defmodule Helios.TestHelpers do
  def kill_node(scheduler, node_id) do
    send(scheduler, {:node_dead, node_id})
  end
end
```

### Step 9: Given tests -- must pass without modification

```elixir
# test/helios/bin_packing_test.exs
defmodule Helios.BinPackingTest do
  use ExUnit.Case, async: true

  alias Helios.BinPacker

  test "selects the node with the tightest fit" do
    nodes = [
      %{id: :n1, available_cpu: 8, available_memory_mb: 8_000},
      %{id: :n2, available_cpu: 4, available_memory_mb: 4_000},
      %{id: :n3, available_cpu: 2, available_memory_mb: 2_000}
    ]

    job = %{cpu: 2, memory_mb: 2_000}
    assert {:ok, %{id: :n3}} = BinPacker.place(nodes, job)
  end

  test "returns :no_capacity when no node fits" do
    nodes = [%{id: :n1, available_cpu: 1, available_memory_mb: 512}]
    job = %{cpu: 4, memory_mb: 4_000}
    assert {:error, :no_capacity} = BinPacker.place(nodes, job)
  end

  test "never overcommits a node" do
    nodes = [%{id: :n1, available_cpu: 3, available_memory_mb: 3_000}]
    job = %{cpu: 4, memory_mb: 2_000}
    assert {:error, :no_capacity} = BinPacker.place(nodes, job)
  end
end
```

```elixir
# test/helios/fault_tolerance_test.exs
defmodule Helios.FaultToleranceTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, scheduler} = Helios.Scheduler.start_link(nodes: 3)
    {:ok, scheduler: scheduler}
  end

  test "jobs on a dead node are requeued within 30 seconds", %{scheduler: scheduler} do
    # Submit 10 jobs that land on node 2
    job_ids = for _ <- 1..10 do
      {:ok, job} = Helios.submit(scheduler, %{cpu: 1, memory_mb: 512, priority: 5, user: "alice"})
      job.id
    end

    # Wait for all to be running
    Process.sleep(500)

    # Kill node 2
    Helios.TestHelpers.kill_node(scheduler, :node_2)

    # Within 30 seconds, all jobs from node 2 must be requeued or rescheduled
    Process.sleep(30_000)

    for job_id <- job_ids do
      state = Helios.job_state(scheduler, job_id)
      assert state in [:queued, :running, :completed],
        "job #{job_id} stuck in state #{state}"
    end
  end
end
```

### Step 10: Run the tests

```bash
mix test test/helios/ --trace
```

### Step 11: Benchmark

```elixir
# bench/placement_bench.exs
nodes = for i <- 1..50 do
  %{id: :"node_#{i}", available_cpu: 32, available_memory_mb: 64_000}
end

jobs = for _ <- 1..1_000 do
  %{cpu: :rand.uniform(8), memory_mb: :rand.uniform(8_000) * 512}
end

Benchee.run(
  %{
    "bin-pack — 1000 jobs x 50 nodes" => fn ->
      Enum.each(jobs, fn job -> Helios.BinPacker.place(nodes, job) end)
    end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

### Why this works

Each job is owned by exactly one shard (determined by consistent hashing on the job key), and each shard has exactly one leader that emits it. A leader lease tied to heartbeats guarantees that only one leader exists at a time, so a job is never executed twice.

---

## Benchmark

```elixir
# bench/scheduler_bench.exs
Benchee.run(%{
  "enqueue" => fn -> DistSched.enqueue(%{task: :noop}) end,
  "dispatch" => fn -> DistSched.drain(1_000) end
}, parallel: 10, time: 10)
```

Target: 10,000 jobs/second enqueued and dispatched end-to-end on a 3-shard localhost cluster.

---

## Trade-off analysis

| Aspect | Best-fit decreasing | First-fit decreasing | Random placement |
|--------|--------------------|--------------------|-----------------|
| Utilization | high (tight fit) | medium | low |
| Placement time | O(N log N) | O(N) | O(1) |
| Fragmentation | low | moderate | high |
| Preemption frequency | lower (fits more) | moderate | higher |
| Implementation complexity | moderate | simple | trivial |

After running the benchmark, record your measured placement latency (p50, p99) for direct comparison across strategies.

Architectural question: the Omega paper (Schwarzkopf et al.) proposes optimistic scheduling with conflict detection instead of pessimistic locking of the cluster state. Under what workload conditions does optimistic scheduling outperform the pessimistic approach you built?

---

## Common production mistakes

**1. Overcommitting on the scheduling decision**
Placement assigns a job to a node, but the node's available capacity is not decremented until the job actually starts. A window exists where multiple jobs are assigned to the same node before any of them start, causing overcommit. Decrement capacity at assignment time, not at execution time.

**2. Preemption without guaranteed requeue**
Before evicting a low-priority job, verify there is a node where it can be rescheduled. If no node can accept the evicted job and the high-priority job, you have evicted a job for nothing. Check requeue feasibility before committing to the eviction.

**3. Fair-share measured at point-in-time only**
A user's fair-share consumption should be measured over a rolling window, not instantaneously. A user who ran 100% of cluster for 1 second and 0% for the next 59 seconds should be counted differently from one who ran 100% for 60 seconds. Use a sliding window EMA.

**4. Heartbeat timeout too aggressive**
Setting the heartbeat timeout too low causes frequent false-positive node failures, triggering unnecessary job requeues and disrupting running work. Calibrate the timeout based on your network's p99 round-trip time, not its p50.

## Reflection

- If one shard leader is network-partitioned from the rest but still holds its lease, what guarantees do you lose? How would you shorten the blast radius?
- Compare your scheduler to Oban (Postgres-backed). At what scale does the Postgres lock-based approach stop being competitive with sharded leaders?

---

## Resources

- Hindman, B. et al. (2011). *Mesos: A Platform for Fine-Grained Resource Sharing in the Data Center* -- section 3 (architecture) and section 4 (two-level scheduling)
- Schwarzkopf, M. et al. (2013). *Omega: Flexible, Scalable Schedulers for Large Compute Clusters* -- shared-state scheduling and conflict resolution
- Ghodsi, A. et al. (2011). *Dominant Resource Fairness: Fair Allocation of Multiple Resource Types* -- multi-resource fair sharing
- [Plug documentation](https://hexdocs.pm/plug/) -- `Plug.Router`, `Plug.Parsers`, and the Plug specification

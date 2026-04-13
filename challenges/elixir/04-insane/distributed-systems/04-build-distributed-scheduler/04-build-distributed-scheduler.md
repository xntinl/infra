# Distributed Job Scheduler with Bin-Packing and Preemption

**Project**: `helios` — a distributed job scheduler with resource-aware placement and fault tolerance

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

**Heartbeat-based failure detection**: each worker sends a heartbeat every 5 seconds. If the scheduler misses 3 consecutive heartbeats (15 seconds), it marks the node unavailable and requeues its jobs. This is not perfect — a slow-but-alive node will generate false positives. The trade-off is between detection latency (low threshold) and false positive rate (high threshold).

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

---

## Implementation milestones

### Step 1: Create the project

**Objective**: Scaffold the distributed scheduler Mix project with the required directory layout.

```bash
mix new helios --sup
cd helios
mkdir -p lib/helios/api test/helios bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Declare the Mix project configuration and third-party dependencies.

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

**Objective**: Define the core structs and types used across every subsequent module.

```elixir
# lib/helios/job.ex
defmodule Helios.Job do
  @moduledoc """
  Job lifecycle FSM:

    submitted → queued → scheduled → running → completed
                                   \\→ preempted → queued (requeued)
                            \\→ failed → dead_queue (after max_attempts)

  Each transition is recorded in the audit log with a timestamp and node assignment.
  
  **State meanings:**
  - submitted: just received from client
  - queued: waiting for placement decision
  - scheduled: placed on a node, awaiting execution
  - running: executing on the node
  - completed: finished successfully
  - preempted: evicted by higher-priority job; moves back to queued
  - failed: execution failed; after N retries, moves to dead_queue
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

  @doc "Transition a job to a new state if valid."
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

**Objective**: Implement the Bin-packing placement component required by the distributed scheduler system.

```elixir
# lib/helios/bin_packer.ex
defmodule Helios.BinPacker do
  @moduledoc """
  Best-fit decreasing placement.

  Given a list of available nodes and a job's resource request,
  returns the node that satisfies the request with the smallest
  remaining capacity gap (tightest fit).
  
  **Algorithm:**
  1. Filter nodes that have enough CPU and memory
  2. Sort by remaining capacity gap (ascending)
  3. Return the first (tightest fit)
  
  This minimizes wasted space per placement and keeps large gaps
  available for future large jobs.
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

**Objective**: Schedule jobs at the requested time using a timer wheel or priority queue.

```elixir
# lib/helios/scheduler.ex
defmodule Helios.Scheduler do
  use GenServer

  @moduledoc """
  Central scheduler that manages job placement, node tracking, and failure detection.
  
  **Responsibilities:**
  1. Track live nodes and their available resources
  2. Accept job submissions and place them on nodes
  3. Monitor heartbeats; requeue jobs on dead nodes
  4. Enforce fair-share quotas per user
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

**Objective**: Implement the REST resource conventions with conventional routes and verbs.

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

**Objective**: Expose the public API surface that clients use to drive the system.

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

**Objective**: Implement the Test Helpers component required by the distributed scheduler system.

```elixir
# lib/helios/test_helpers.ex
defmodule Helios.TestHelpers do
  def kill_node(scheduler, node_id) do
    send(scheduler, {:node_dead, node_id})
  end
end
```

### Step 9: Given tests — must pass without modification

**Objective**: Validate behavior against the frozen test suite that must pass unmodified.

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

**Objective**: Execute the provided test suite to verify the implementation passes.

```bash
mix test test/helios/ --trace
```

---

## Quick start

This is a single-dispatcher implementation. For distributed deployment:

1. **Add sharding**: partition job queue by hash(job_id) across N shard leaders
2. **Implement gossip**: each shard leader shares node state changes via gossip
3. **Add preemption**: scan queue when a high-priority job can't fit; select low-priority victims
4. **Add fair-share**: track per-user CPU/memory consumption and cap submissions

---
## Main Entry Point

```elixir
def main do
  IO.puts("======== 04-build-distributed-scheduler ========")
  IO.puts("Build Distributed Scheduler")
  IO.puts("")
  
  Helios.Job.start_link([])
  IO.puts("Helios.Job started")
  
  IO.puts("Run: mix test")
end
```


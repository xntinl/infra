# 2. GenServer handle_continue & Lazy Recovery

**Difficulty**: Avanzado

## Prerequisites
- Mastered: GenServer lifecycle, `init/1`, `handle_call/3`, `handle_cast/2`
- Mastered: ETS basics — table creation, `:ets.lookup/2`, `:ets.insert/2`
- Familiarity with: Supervisor restart strategies, process linking, crash semantics

## Learning Objectives
- Analyze the difference between blocking `init/1` and deferred `handle_continue/2`
- Design lazy-initialization patterns that keep supervisor startup time predictable
- Evaluate when async recovery from ETS is safer than synchronous DB loading
- Implement the `{:reply, result, state, {:continue, key}}` pattern correctly

## Concepts

### Why handle_continue Exists

Before `handle_continue/2` (added in OTP 21), developers faced a dilemma: expensive
initialization work (DB queries, file reads, cache warm-up) had to happen either in
`init/1` — blocking the supervisor until done — or via a `send(self(), :init)` trick
that opened a race window where the process could receive external messages before
its internal state was ready.

`handle_continue/2` closes that race. It runs AFTER the current callback returns and
BEFORE any new message from the mailbox is processed. The process is fully registered,
its supervisor considers it started, but no external message can jump the queue. It is
the safe, first-class mechanism for deferred initialization.

```
Supervisor calls start_link
  └─ init/1 returns {:ok, state, {:continue, :load_data}}
       └─ Supervisor continues (process is "started")
            └─ handle_continue(:load_data, state) runs immediately
                 └─ state is now fully populated
                      └─ first external message handled HERE
```

```elixir
defmodule LazyWorker do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(opts) do
    # Return immediately — supervisor startup is not blocked
    {:ok, %{config: opts, data: nil, ready: false}, {:continue, :load_initial_data}}
  end

  def handle_continue(:load_initial_data, state) do
    data = ExpensiveDataSource.load(state.config)
    {:noreply, %{state | data: data, ready: true}}
  end

  def handle_call(:get_data, _from, %{ready: false} = state) do
    {:reply, {:error, :not_ready}, state}
  end

  def handle_call(:get_data, _from, state) do
    {:reply, {:ok, state.data}, state}
  end
end
```

### Chaining handle_continue

`handle_continue/2` can itself return `{:noreply, state, {:continue, :next_step}}`,
creating a chain of deferred steps. This models multi-phase initialization cleanly
without nesting callbacks or building ad-hoc state machines.

Use chaining when each step depends on the previous one and all steps must complete
before external messages are safe to process. Avoid chaining for independent steps —
parallel initialization via `Task.async` + `Task.await` in a single `handle_continue`
is faster.

```elixir
def handle_continue(:phase_one, state) do
  result = do_phase_one()
  {:noreply, %{state | phase_one: result}, {:continue, :phase_two}}
end

def handle_continue(:phase_two, state) do
  result = do_phase_two(state.phase_one)
  {:noreply, %{state | phase_two: result}}
  # No further continue — normal message handling begins
end
```

### State Recovery After Crash

Supervisors restart crashed GenServers by calling `start_link` fresh — the process
gets a clean `init/1`. Any in-memory state is gone. To survive crashes, GenServers
must externalize state: ETS (survives process death if the table owner is a different
process or uses `:heir`), Mnesia, a database, or a persistent file.

`handle_continue` is the natural place to rebuild state post-crash:

1. `init/1` runs, detects a prior ETS snapshot exists
2. Returns `{:ok, empty_state, {:continue, :recover_from_ets}}`
3. `handle_continue(:recover_from_ets, state)` loads the snapshot
4. Process is now fully recovered before its first external call

```elixir
def init(id) do
  base_state = %{id: id, entries: %{}, version: 0}
  continuation =
    if ets_snapshot_exists?(id),
      do: {:continue, :recover},
      else: :ignore |> then(fn _ -> nil end)

  case continuation do
    {:continue, _} = c -> {:ok, base_state, c}
    _ -> {:ok, base_state}
  end
end
```

### The Reply + Continue Pattern

A GenServer can respond to a caller AND then continue processing without the caller
waiting. This is useful when the response is known immediately but the side effect
takes longer:

```elixir
def handle_call({:submit_job, job}, _from, state) do
  job_id = UUID.generate()
  new_state = %{state | pending: [job_id | state.pending]}
  # Reply immediately with job_id, then process in handle_continue
  {:reply, {:ok, job_id}, new_state, {:continue, {:process_job, job_id}}}
end

def handle_continue({:process_job, job_id}, state) do
  result = do_expensive_work(state.pending_jobs[job_id])
  {:noreply, update_state(state, job_id, result)}
end
```

### Trade-offs

| Approach | Startup Time | Race Safety | Complexity |
|---|---|---|---|
| All work in `init/1` | Slow (blocks supervisor) | Safe | Low |
| `send(self(), :init)` (old hack) | Fast | Unsafe (race window) | Medium |
| `handle_continue` from `init` | Fast | Safe | Low |
| `Task.async` in `handle_continue` | Fast | Safe | Medium |
| External state + recovery | Fast | Safe | High |

---

## Exercises

### Exercise 1: Lazy Database Initialization

**Problem**: You have a `ProductCatalog` GenServer that serves product lookup requests.
It needs to load 50,000 products from a database at startup. Loading takes ~2 seconds.
Your application has 12 such GenServers (one per product category). A blocking `init/1`
makes your supervisor take 24 seconds to start — unacceptable.

Implement lazy initialization: `start_link` returns immediately, and the DB load happens
in `handle_continue`. Callers that arrive before loading completes receive
`{:error, :loading}` with a retry hint. Once loaded, normal service resumes.

**Requirements**:
- `init/1` completes in under 5ms
- Loading is simulated with `Process.sleep(2_000)` + building a map
- Callers during load receive `{:error, :loading}` — not a crash, not a timeout
- After load completes, all calls work normally
- Expose `ready?/1` function for health-check polling

**Hints**:
- Keep `:ready` boolean in state. Pattern match on it in `handle_call`
- The simulated DB load in `handle_continue` can just be `Process.sleep(2_000)` followed
  by populating a map with fake data — the structure matters, not the source
- Supervisors check that `init/1` returns — they do not wait for `handle_continue`.
  Your test can call `ready?/1` in a retry loop to know when the server is done

**One possible solution**:
```elixir
defmodule ProductCatalog do
  use GenServer
  require Logger

  def start_link(category) do
    GenServer.start_link(__MODULE__, category, name: via(category))
  end

  def lookup(category, product_id) do
    GenServer.call(via(category), {:lookup, product_id})
  end

  def ready?(category) do
    GenServer.call(via(category), :ready?)
  end

  def init(category) do
    Logger.info("ProductCatalog[#{category}] starting — data load deferred")
    state = %{category: category, products: %{}, ready: false}
    {:ok, state, {:continue, :load_products}}
  end

  def handle_continue(:load_products, state) do
    Logger.info("ProductCatalog[#{state.category}] loading products...")
    # Simulate expensive DB load
    Process.sleep(2_000)
    products = build_fake_catalog(state.category, 50_000)
    Logger.info("ProductCatalog[#{state.category}] ready (#{map_size(products)} products)")
    {:noreply, %{state | products: products, ready: true}}
  end

  def handle_call(:ready?, _from, state) do
    {:reply, state.ready, state}
  end

  def handle_call({:lookup, _id}, _from, %{ready: false} = state) do
    {:reply, {:error, :loading}, state}
  end

  def handle_call({:lookup, product_id}, _from, state) do
    result = Map.get(state.products, product_id, {:error, :not_found})
    {:reply, result, state}
  end

  defp via(category), do: {:via, Registry, {ProductRegistry, category}}

  defp build_fake_catalog(category, n) do
    for i <- 1..n, into: %{} do
      {"#{category}_#{i}", %{id: "#{category}_#{i}", price: :rand.uniform(10_000)}}
    end
  end
end
```

---

### Exercise 2: ETS-Backed Crash Recovery

**Problem**: You have a `SessionStore` GenServer that holds active user sessions.
Sessions accumulate during the day. If the process crashes and a supervisor restarts
it, all session data is lost — users are logged out. Design a crash recovery mechanism:
session writes are mirrored to ETS immediately (so ETS is always fresh), and on restart
`handle_continue` reads the ETS snapshot to restore full state without any DB call.

**Requirements**:
- ETS table named `:session_store` is created by the GenServer with `heir: :none`
  — this means the table is transferred to no process on death, so it survives the
  crash (the table lives until explicitly deleted)
- On `init/1`, check if `:session_store` ETS table already exists
  - If yes → return `{:ok, state, {:continue, :recover}}`
  - If no → create the table, return `{:ok, state}`
- `handle_continue(:recover, state)` loads all entries from ETS into state
- All `put_session/2` calls update both the GenServer state and ETS atomically
  (within the callback — not truly atomic, but single-writer so safe)

**Hints**:
- `:ets.info(:session_store)` returns `:undefined` if the table does not exist
- Use `:ets.tab2list/1` to load all entries at once during recovery
- The ETS table must be created with appropriate options so it survives the process —
  use `:public` (or `:protected`) and specify a fixed name, not a dynamic one
- To test crash recovery: use `Process.exit(pid, :kill)` and wait for the supervisor
  to restart the process, then call `get_session/1` and verify data is intact

**One possible solution**:
```elixir
defmodule SessionStore do
  use GenServer

  @table :session_store

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  def put_session(session_id, data) do
    GenServer.call(__MODULE__, {:put, session_id, data})
  end

  def get_session(session_id) do
    GenServer.call(__MODULE__, {:get, session_id})
  end

  def init(_opts) do
    case :ets.info(@table) do
      :undefined ->
        :ets.new(@table, [:named_table, :set, :public])
        {:ok, %{sessions: %{}, recovered: false}}

      _info ->
        # Table exists from before the crash
        {:ok, %{sessions: %{}, recovered: false}, {:continue, :recover}}
    end
  end

  def handle_continue(:recover, state) do
    sessions =
      @table
      |> :ets.tab2list()
      |> Map.new(fn {k, v} -> {k, v} end)

    require Logger
    Logger.info("SessionStore recovered #{map_size(sessions)} sessions from ETS")
    {:noreply, %{state | sessions: sessions, recovered: true}}
  end

  def handle_call({:put, sid, data}, _from, state) do
    :ets.insert(@table, {sid, data})
    {:reply, :ok, put_in(state, [:sessions, sid], data)}
  end

  def handle_call({:get, sid}, _from, state) do
    {:reply, Map.get(state.sessions, sid, nil), state}
  end
end
```

---

### Exercise 3: Reply-Then-Continue Async Processing

**Problem**: You have a `ReportGenerator` GenServer. Clients submit report jobs and
expect an immediate `{:ok, job_id}` acknowledgment — they will poll for results later.
However, the actual report generation takes 5–10 seconds. If generation happens
synchronously inside `handle_call`, the entire GenServer is blocked for that duration,
starving other callers. Use `handle_continue` to acknowledge immediately and process
asynchronously in the background.

**Requirements**:
- `submit_report/1` returns `{:ok, job_id}` in under 1ms
- Generation is simulated with `Process.sleep(5_000)`
- `get_result/1` returns `{:pending}` while generating, `{:ok, report}` when done
- The GenServer must remain responsive to `get_result` calls during generation
- Queue multiple concurrent submissions correctly (FIFO processing)

**Hints**:
- `{:reply, {:ok, job_id}, new_state, {:continue, {:generate, job_id}}}` is the
  key pattern — the reply goes to the caller, then generation runs in `handle_continue`
- While `handle_continue` is running, new messages queue in the mailbox and are
  processed after it returns — the GenServer IS blocked during generation. To allow
  true concurrency you must spawn a Task from within `handle_continue`. This is an
  important nuance: `handle_continue` is still sequential, just deferred
- For the concurrent version: `Task.async(fn -> generate(job) end)`, then handle
  `{:DOWN, ...}` or the Task's result message in `handle_info/2`

**One possible solution**:
```elixir
defmodule ReportGenerator do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def submit_report(params) do
    GenServer.call(__MODULE__, {:submit, params})
  end

  def get_result(job_id) do
    GenServer.call(__MODULE__, {:result, job_id})
  end

  def init(_) do
    {:ok, %{jobs: %{}, queue: :queue.new()}}
  end

  def handle_call({:submit, params}, _from, state) do
    job_id = generate_id()
    jobs = Map.put(state.jobs, job_id, :pending)
    queue = :queue.in({job_id, params}, state.queue)
    new_state = %{state | jobs: jobs, queue: queue}
    {:reply, {:ok, job_id}, new_state, {:continue, :process_next}}
  end

  def handle_call({:result, job_id}, _from, state) do
    result = Map.get(state.jobs, job_id, {:error, :unknown_job})
    {:reply, result, state}
  end

  def handle_continue(:process_next, state) do
    case :queue.out(state.queue) do
      {:empty, _} ->
        {:noreply, state}

      {{:value, {job_id, params}}, rest_queue} ->
        # Spawn Task so GenServer remains responsive during generation
        task = Task.async(fn -> do_generate(params) end)
        new_state = %{state |
          queue: rest_queue,
          jobs: Map.put(state.jobs, job_id, {:in_progress, task})
        }
        {:noreply, new_state}
    end
  end

  # Receive Task result
  def handle_info({ref, result}, state) when is_reference(ref) do
    Process.demonitor(ref, [:flush])
    job_id =
      Enum.find_value(state.jobs, fn
        {id, {:in_progress, %Task{ref: ^ref}}} -> id
        _ -> nil
      end)

    {:noreply, put_in(state, [:jobs, job_id], {:ok, result})}
  end

  defp do_generate(_params) do
    Process.sleep(5_000)
    %{generated_at: DateTime.utc_now(), data: "report_data"}
  end

  defp generate_id, do: :crypto.strong_rand_bytes(8) |> Base.encode16()
end
```

---

## Common Mistakes

### Mistake: Blocking init/1 With Expensive Work

Putting a 2-second DB call directly in `init/1` blocks the supervisor. If 10 workers
start concurrently under a `DynamicSupervisor`, the supervisor is effectively serialized.
The supervisor's own `start_child/2` call blocks until `init/1` returns. For slow
initialization, always delegate to `handle_continue`.

### Mistake: Using send(self(), :init) Instead of handle_continue

The `send(self(), :init)` pattern predates `handle_continue`. It has a real race
condition: if another process sends a message to the new GenServer BEFORE the `:init`
message is processed, that external message runs before initialization is complete.
`handle_continue` guarantees ordering — it always runs before the next mailbox message.

### Mistake: Long handle_continue Chains Without Error Handling

A `handle_continue` chain that crashes partway leaves the process in a half-initialized
state. If the crash is unhandled, the supervisor restarts the process — but if `init/1`
does not detect the partial state, you may end up in the same partial initialization
loop. Design crash-safe recovery: each step should be idempotent or include a rollback.

### Mistake: Assuming handle_continue Makes the GenServer Non-Blocking

`handle_continue` runs in the GenServer's own process. While it is running, no other
messages are processed. It is "deferred" only in the sense that the previous callback
has already returned. A 5-second `handle_continue` blocks the GenServer for 5 seconds.
Spawn a Task from within `handle_continue` if you need true concurrency.

---

## Summary
- `handle_continue` eliminates the race condition of `send(self(), :init)` and the
  startup cost of blocking `init/1` — use it for all expensive deferred initialization
- Chain continue steps for sequential multi-phase init; use Tasks for parallel work
- ETS as a crash-safe mirror is a robust recovery pattern for in-memory state
- `{:reply, result, state, {:continue, work}}` lets callers get answers while work
  continues — but the GenServer is still sequential; spawn Tasks for true concurrency

## What's Next
Exercise 03 — Timeouts & Heartbeats: build on GenServer's timeout mechanism to detect
dead dependencies and implement circuit-breaker health tracking.

## Resources
- OTP 21 release notes — `handle_continue/2` introduction
- Erlang/OTP docs: `gen_server` module — https://www.erlang.org/doc/man/gen_server.html
- Saša Jurić — "Elixir in Action" 2nd ed., ch. 7 (GenServer internals)
- Fred Hébert — "Learn You Some Erlang" — Error Handling chapter

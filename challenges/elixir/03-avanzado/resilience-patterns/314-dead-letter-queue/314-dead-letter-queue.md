# Dead Letter Queue Patterns

**Project**: `webhook_dlq` — a webhook delivery system that retries failed deliveries with backoff and, after exhausting attempts, parks undeliverable messages in a Dead Letter Queue for human inspection.

## Project context

You push webhook events to customer endpoints. Customer endpoints fail: DNS outages, certificate expiry, 500s, customers who deleted the URL without telling you. Your choices for a failed delivery are:

1. Drop silently — customer loses data, you never know.
2. Retry forever — one bad customer poisons the queue for everyone.
3. Retry N times then move to a DLQ — failed messages are preserved, the main queue stays healthy, an operator can inspect/replay/purge.

Option 3 is the industry standard (SQS, RabbitMQ, Kafka with a DLQ topic). This exercise implements it in pure Elixir with bounded retries, classification of errors, and operator-facing inspect/replay/drop operations.

```
webhook_dlq/
├── lib/
│   └── webhook_dlq/
│       ├── application.ex
│       ├── main_queue.ex
│       ├── dlq.ex
│       └── deliverer.ex            # worker that moves between queues
├── test/
│   └── webhook_dlq/
│       └── dlq_test.exs
└── mix.exs
```

## Why a DLQ and not infinite retries

A bad recipient that always 500s will be retried by every worker forever. Workers that could be delivering other messages instead spend their time on a guaranteed-failure. Throughput collapses. A DLQ parks these messages out of the hot path and keeps workers productive.

## Why retry *then* DLQ and not DLQ immediately

Not every failure is permanent. Transient errors (503, timeout, DNS blip) resolve in seconds. Immediate-DLQ would flood the DLQ with recoverable failures and require manual replay for routine glitches. Retry with backoff handles the transient cases; DLQ handles the persistent ones.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. Retry budget
Each message has an `attempts` counter and a `max_attempts`. On failure, worker re-enqueues with incremented `attempts` and a scheduled `visible_after`. When `attempts >= max_attempts`, the message moves to DLQ instead.

### 2. Error classification
```
:transient  → retry  (503, timeout, econnrefused)
:permanent  → DLQ    (410, 401, invalid_payload)
```
Permanent errors skip retries entirely. They'll never succeed. Park them immediately.

### 3. DLQ operations
- `DLQ.list/0` — inspect
- `DLQ.replay(id)` — move back to main queue, reset attempts
- `DLQ.drop(id)` — delete permanently
- `DLQ.purge/0` — drop all

## Design decisions

- **Option A — Single queue with a status field**: simpler, but scans the whole queue to find active vs. dead.
- **Option B — Two queues (main, DLQ)**: two physical structures, clear separation.
→ Chose **B**. O(1) find-next-work regardless of DLQ size.

- **Option A — In-memory queues (`:queue`)**: fastest, lost on restart.
- **Option B — ETS-backed**: fast, inspectable, still lost on restart.
- **Option C — Disk-backed**: durable, slow.
→ Chose **B** for this exercise; production would be C or external (SQS, RabbitMQ).

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule WebhookDlq.MixProject do
  use Mix.Project
  def project, do: [app: :webhook_dlq, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [mod: {WebhookDlq.Application, []}, extra_applications: [:logger]]
end
```

### Step 1: Application

**Objective**: Wire main queue, DLQ, and deliverer under one_for_one so max_attempts retry budget and DLQ threshold are operator-configurable at startup.

```elixir
defmodule WebhookDlq.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      WebhookDlq.MainQueue,
      WebhookDlq.DLQ,
      {WebhookDlq.Deliverer, max_attempts: 3}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Main queue (`lib/webhook_dlq/main_queue.ex`)

**Objective**: Use :ordered_set keyed by monotonic_time so FIFO dequeue is O(log n) and first/1 returns head without scanning full table.

```elixir
defmodule WebhookDlq.MainQueue do
  use GenServer

  @table :webhook_main

  def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def enqueue(message), do: GenServer.call(__MODULE__, {:enqueue, message})
  def dequeue, do: GenServer.call(__MODULE__, :dequeue)
  def size, do: :ets.info(@table, :size)
  def list, do: :ets.tab2list(@table) |> Enum.map(&elem(&1, 1))

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :ordered_set])
    {:ok, %{}}
  end

  @impl true
  def handle_call({:enqueue, msg}, _from, state) do
    msg = Map.put_new(msg, :attempts, 0)
    id = {System.monotonic_time(), System.unique_integer([:monotonic])}
    :ets.insert(@table, {id, msg})
    {:reply, :ok, state}
  end

  def handle_call(:dequeue, _from, state) do
    case :ets.first(@table) do
      :"$end_of_table" ->
        {:reply, :empty, state}

      id ->
        [{^id, msg}] = :ets.lookup(@table, id)
        :ets.delete(@table, id)
        {:reply, {:ok, msg}, state}
    end
  end
end
```

### Step 3: DLQ (`lib/webhook_dlq/dlq.ex`)

**Objective**: Stash permanent failures with reason and expose replay/drop operations so operators recover poisoned messages without code changes.

```elixir
defmodule WebhookDlq.DLQ do
  use GenServer
  alias WebhookDlq.MainQueue

  @table :webhook_dlq

  def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def park(message, reason), do: GenServer.call(__MODULE__, {:park, message, reason})
  def list, do: :ets.tab2list(@table) |> Enum.map(fn {id, m, r} -> %{id: id, message: m, reason: r} end)
  def replay(id), do: GenServer.call(__MODULE__, {:replay, id})
  def drop(id), do: GenServer.call(__MODULE__, {:drop, id})
  def purge, do: GenServer.call(__MODULE__, :purge)
  def size, do: :ets.info(@table, :size)

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :set])
    {:ok, %{}}
  end

  @impl true
  def handle_call({:park, msg, reason}, _from, state) do
    id = System.unique_integer([:positive, :monotonic])
    :ets.insert(@table, {id, msg, reason})
    {:reply, {:ok, id}, state}
  end

  def handle_call({:replay, id}, _from, state) do
    case :ets.take(@table, id) do
      [{^id, msg, _reason}] ->
        reset = Map.put(msg, :attempts, 0)
        :ok = MainQueue.enqueue(reset)
        {:reply, :ok, state}

      [] ->
        {:reply, {:error, :not_found}, state}
    end
  end

  def handle_call({:drop, id}, _from, state) do
    case :ets.take(@table, id) do
      [_] -> {:reply, :ok, state}
      [] -> {:reply, {:error, :not_found}, state}
    end
  end

  def handle_call(:purge, _from, state) do
    :ets.delete_all_objects(@table)
    {:reply, :ok, state}
  end
end
```

### Step 4: Deliverer (`lib/webhook_dlq/deliverer.ex`)

**Objective**: Classify failures as transient (retry) vs. permanent (park in DLQ) and increment attempts counter so max_attempts exhaustion moves message out of main queue.

```elixir
defmodule WebhookDlq.Deliverer do
  use GenServer
  alias WebhookDlq.{MainQueue, DLQ}

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def tick, do: GenServer.call(__MODULE__, :tick)
  def set_fake_sender(fun), do: GenServer.call(__MODULE__, {:set_sender, fun})

  @impl true
  def init(opts) do
    {:ok, %{max_attempts: Keyword.fetch!(opts, :max_attempts), sender: &default_sender/1}}
  end

  @impl true
  def handle_call(:tick, _from, state) do
    case MainQueue.dequeue() do
      :empty ->
        {:reply, :idle, state}

      {:ok, msg} ->
        handle_message(msg, state)
        {:reply, :processed, state}
    end
  end

  def handle_call({:set_sender, fun}, _from, state) do
    {:reply, :ok, %{state | sender: fun}}
  end

  defp handle_message(msg, %{max_attempts: max, sender: sender}) do
    case sender.(msg) do
      :ok ->
        :delivered

      {:error, :permanent, reason} ->
        DLQ.park(msg, {:permanent, reason})

      {:error, :transient, reason} ->
        next_attempts = msg.attempts + 1

        if next_attempts >= max do
          DLQ.park(msg, {:max_attempts_exceeded, reason})
        else
          MainQueue.enqueue(%{msg | attempts: next_attempts})
        end
    end
  end

  defp default_sender(_msg), do: :ok
end
```

## Why this works

- **Separate queues enforce separation of concerns** — the deliverer never scans the DLQ looking for work. O(1) dequeue from the live queue regardless of how many dead messages exist.
- **Permanent classification skips retries** — a 410 Gone is never going to succeed. Sending it to DLQ immediately saves 3 attempts' worth of work.
- **Replay resets attempts** — an operator who replays a DLQ message should have the full retry budget again, not inherit a nearly-exhausted counter.
- **`:ets.take/2` is atomic** — grabbing the message and removing it in one op prevents concurrent drop+replay races.
- **Deliverer is stateless on retries** — a transient failure re-enqueues with updated attempts. The deliverer process has no per-message state to lose on restart.

## Tests

```elixir
defmodule WebhookDlq.DlqTest do
  use ExUnit.Case, async: false
  alias WebhookDlq.{Deliverer, DLQ, MainQueue}

  setup do
    :ets.delete_all_objects(:webhook_main)
    :ets.delete_all_objects(:webhook_dlq)
    :ok
  end

  describe "happy path" do
    test "successful delivery leaves nothing in queues" do
      Deliverer.set_fake_sender(fn _ -> :ok end)
      MainQueue.enqueue(%{id: 1, url: "https://ok"})
      :processed = Deliverer.tick()

      assert 0 == MainQueue.size()
      assert 0 == DLQ.size()
    end
  end

  describe "transient failures" do
    test "retries up to max_attempts then DLQ" do
      Deliverer.set_fake_sender(fn _ -> {:error, :transient, :timeout} end)
      MainQueue.enqueue(%{id: 1, url: "https://flaky"})

      Deliverer.tick()
      Deliverer.tick()
      Deliverer.tick()

      assert 0 == MainQueue.size()
      assert 1 == DLQ.size()

      [%{reason: {:max_attempts_exceeded, :timeout}}] = DLQ.list()
    end
  end

  describe "permanent failures" do
    test "DLQ immediately, skipping retries" do
      Deliverer.set_fake_sender(fn _ -> {:error, :permanent, :gone_410} end)
      MainQueue.enqueue(%{id: 1, url: "https://gone"})

      :processed = Deliverer.tick()

      assert 0 == MainQueue.size()
      assert 1 == DLQ.size()
    end
  end

  describe "replay" do
    test "moves back to main queue with reset attempts" do
      {:ok, id} = DLQ.park(%{id: 7, url: "https://x", attempts: 3}, {:permanent, :gone})
      :ok = DLQ.replay(id)

      assert 0 == DLQ.size()
      assert 1 == MainQueue.size()
      [%{attempts: 0}] = MainQueue.list()
    end
  end

  describe "drop and purge" do
    test "drop removes by id" do
      {:ok, id} = DLQ.park(%{id: 1}, :reason)
      :ok = DLQ.drop(id)
      assert 0 == DLQ.size()
    end

    test "purge clears everything" do
      for i <- 1..5, do: DLQ.park(%{id: i}, :r)
      assert 5 == DLQ.size()
      :ok = DLQ.purge()
      assert 0 == DLQ.size()
    end
  end
end
```

## Benchmark

```elixir
# Enqueue + dequeue throughput — should exceed 100k ops/s on a laptop.
{:ok, _} = Application.ensure_all_started(:webhook_dlq)

n = 100_000
{t, _} = :timer.tc(fn ->
  for i <- 1..n, do: WebhookDlq.MainQueue.enqueue(%{id: i})
  for _ <- 1..n, do: WebhookDlq.MainQueue.dequeue()
end)
IO.puts("#{n * 2 / (t / 1_000_000)} ops/s")
```

Expected: > 200k ops/s for enqueue+dequeue pairs.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Restart loses in-flight messages** — ETS dies with the owner. Production DLQ must persist (Oban with Postgres, external MQ). This exercise is in-memory.

**2. DLQ that nobody watches is a leak** — alert when DLQ size > threshold. A DLQ that no one inspects is the same as dropping messages.

**3. Classification logic is the hardest part** — "was this a transient 500 or a genuine bug?" In practice, log everything, start with small max_attempts (3), and expand based on observed patterns.

**4. Replay can re-trigger side effects** — if the webhook delivered partially (the customer's system logged it) but returned an error, replay re-sends. Make the consumer idempotent.

**5. Ordered_set in main queue trades write speed for FIFO** — O(log n) insert vs. O(1) for a raw queue. Fine up to millions of messages; above that, use a dedicated queue library.

**6. When NOT to use this** — fire-and-forget metrics, telemetry, log shipping: acceptable to drop. A DLQ adds operational burden; only use it where each message matters.

## Reflection

An operator replays 1000 DLQ messages at once during an incident recovery. What happens to main queue throughput? How would you rate-limit the replay?

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      # No external dependencies — pure Elixir
    ]
  end

  defmodule WebhookDlq.MixProject do
    end
    use Mix.Project
    def project, do: [app: :webhook_dlq, version: "0.1.0", elixir: "~> 1.17", deps: []]
    def application, do: [mod: {WebhookDlq.Application, []}, extra_applications: [:logger]]
  end

  defmodule WebhookDlq.Application do
    use Application

    @impl true
    def start(_type, _args) do
      children = [
        WebhookDlq.MainQueue,
        WebhookDlq.DLQ,
        {WebhookDlq.Deliverer, max_attempts: 3}
      ]

      Supervisor.start_link(children, strategy: :one_for_one)
    end
  end

  defmodule WebhookDlq.MainQueue do
    end
    use GenServer

    @table :webhook_main

    def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

    def enqueue(message), do: GenServer.call(__MODULE__, {:enqueue, message})
    def dequeue, do: GenServer.call(__MODULE__, :dequeue)
    def size, do: :ets.info(@table, :size)
    def list, do: :ets.tab2list(@table) |> Enum.map(&elem(&1, 1))

    @impl true
    def init(_) do
      :ets.new(@table, [:named_table, :public, :ordered_set])
      {:ok, %{}}
    end

    @impl true
    def handle_call({:enqueue, msg}, _from, state) do
      msg = Map.put_new(msg, :attempts, 0)
      id = {System.monotonic_time(), System.unique_integer([:monotonic])}
      :ets.insert(@table, {id, msg})
      {:reply, :ok, state}
    end

    def handle_call(:dequeue, _from, state) do
      case :ets.first(@table) do
        :"$end_of_table" ->
          {:reply, :empty, state}

        id ->
          [{^id, msg}] = :ets.lookup(@table, id)
          :ets.delete(@table, id)
          {:reply, {:ok, msg}, state}
      end
    end
  end

  defmodule WebhookDlq.DLQ do
    end
    use GenServer
    alias WebhookDlq.MainQueue

    @table :webhook_dlq

    def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

    def park(message, reason), do: GenServer.call(__MODULE__, {:park, message, reason})
    def list, do: :ets.tab2list(@table) |> Enum.map(fn {id, m, r} -> %{id: id, message: m, reason: r} end)
    def replay(id), do: GenServer.call(__MODULE__, {:replay, id})
    def drop(id), do: GenServer.call(__MODULE__, {:drop, id})
    def purge, do: GenServer.call(__MODULE__, :purge)
    def size, do: :ets.info(@table, :size)

    @impl true
    def init(_) do
      :ets.new(@table, [:named_table, :public, :set])
      {:ok, %{}}
    end

    @impl true
    def handle_call({:park, msg, reason}, _from, state) do
      id = System.unique_integer([:positive, :monotonic])
      :ets.insert(@table, {id, msg, reason})
      {:reply, {:ok, id}, state}
    end

    def handle_call({:replay, id}, _from, state) do
      case :ets.take(@table, id) do
        [{^id, msg, _reason}] ->
          reset = Map.put(msg, :attempts, 0)
          :ok = MainQueue.enqueue(reset)
          {:reply, :ok, state}

        [] ->
          {:reply, {:error, :not_found}, state}
      end
    end

    def handle_call({:drop, id}, _from, state) do
      case :ets.take(@table, id) do
        [_] -> {:reply, :ok, state}
        [] -> {:reply, {:error, :not_found}, state}
      end
    end

    def handle_call(:purge, _from, state) do
      :ets.delete_all_objects(@table)
      {:reply, :ok, state}
    end
  end

  defmodule WebhookDlq.Deliverer do
    end
    use GenServer
    alias WebhookDlq.{MainQueue, DLQ}

    def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

    def tick, do: GenServer.call(__MODULE__, :tick)
    def set_fake_sender(fun), do: GenServer.call(__MODULE__, {:set_sender, fun})

    @impl true
    def init(opts) do
      {:ok, %{max_attempts: Keyword.fetch!(opts, :max_attempts), sender: &default_sender/1}}
    end

    @impl true
    def handle_call(:tick, _from, state) do
      case MainQueue.dequeue() do
        :empty ->
          {:reply, :idle, state}

        {:ok, msg} ->
          handle_message(msg, state)
          {:reply, :processed, state}
      end
    end

    def handle_call({:set_sender, fun}, _from, state) do
      {:reply, :ok, %{state | sender: fun}}
    end

    defp handle_message(msg, %{max_attempts: max, sender: sender}) do
      case sender.(msg) do
        :ok ->
          :delivered

        {:error, :permanent, reason} ->
          DLQ.park(msg, {:permanent, reason})

        {:error, :transient, reason} ->
          next_attempts = msg.attempts + 1

          if next_attempts >= max do
            DLQ.park(msg, {:max_attempts_exceeded, reason})
          else
            MainQueue.enqueue(%{msg | attempts: next_attempts})
          end
      end
    end

    defp default_sender(_msg), do: :ok
  end

  defmodule Main do
    def main do
        # Demonstrating 314-dead-letter-queue
        :ok
    end
  end

  Main.main()
  end
  end
  end
  end
  end
  end
  end
  end
  end
  end
  end
  end
  end
end

Main.main()
```

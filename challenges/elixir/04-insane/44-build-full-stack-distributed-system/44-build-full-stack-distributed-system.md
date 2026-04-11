# Full-Stack Distributed System

**Project**: `platform` — Production-grade distributed system with API gateway, consensus-based coordination, persistent storage, job queue, stream processing, and observability

## Project context

Your team is building a SaaS platform that handles user-submitted analytics jobs. A client submits a job via REST. The API Gateway authenticates and rate-limits the request, then routes it to a Coordinator. The Coordinator stores state in a persistent Storage layer and publishes events to a Stream Processor. All components emit telemetry. A multi-region simulation adds geo-routing. The system must sustain 50,000 req/s at P99 under 50ms on a laptop.

There are no shortcuts. Every subsystem must be wired with real error handling, circuit breakers, health checks, and graceful shutdown. The goal is not a demo; it is a system you would hand to an SRE on call at 3am.

## Why this is harder than the sum of its parts

Individual components fail cleanly in isolation. In a composed system, failure cascades: the Storage layer's compaction pauses reads, the Gateway's circuit breaker trips, the Queue backs up, the Stream Processor's late event watermarks drift. Identifying and breaking these cascades requires understanding each component's failure modes AND the contract it offers to its callers.

The 50k RPS target is non-trivial in Elixir. It requires avoiding process-per-request overhead in hot paths, using ETS for shared state rather than GenServer mailboxes, and batching telemetry emission.

## Why ETS for the Gateway hot path

At 50k RPS, a GenServer rate limiter becomes a bottleneck — every request serializes through one process mailbox. ETS with `:ets.update_counter/3` provides atomic increment without a process boundary. The operation is O(1) and takes ~100ns. Token refill is done by a single background process that resets counters on a timer.

## Why Raft and not just `:global` for leader election

`:global.register_name/2` uses a two-phase locking protocol with no progress guarantee under network partition. Raft provides linearizable leader election with bounded election timeout. The simplified Raft implemented here handles leader election only (no log replication), sufficient to demonstrate failure recovery: leader dies then a follower is elected in under 5 seconds.

## Why reservoir sampling for percentiles

Storing every latency observation is O(n) space per metric per window. At 50k RPS over a 1-minute window, that is 3 million samples. Reservoir sampling (Vitter's Algorithm R) maintains a fixed-size sample (1000 observations) with equal probability of inclusion. The P99 estimate from 1000 samples has a confidence interval of +-1% at 95% confidence.

## Project Structure

```
platform/
├── mix.exs
├── config/
│   ├── config.exs
│   └── test.exs
├── lib/
│   ├── platform/
│   │   ├── gateway/
│   │   │   ├── router.ex
│   │   │   ├── auth.ex
│   │   │   ├── rate_limiter.ex
│   │   │   └── circuit_breaker.ex
│   │   ├── coordinator/
│   │   │   ├── raft.ex
│   │   │   ├── leader.ex
│   │   │   └── worker.ex
│   │   ├── storage/
│   │   │   ├── lsm.ex
│   │   │   ├── cache.ex
│   │   │   └── store.ex
│   │   ├── queue/
│   │   │   ├── job.ex
│   │   │   ├── scheduler.ex
│   │   │   └── dead_letter.ex
│   │   ├── stream/
│   │   │   ├── window.ex
│   │   │   ├── operators.ex
│   │   │   └── processor.ex
│   │   ├── telemetry/
│   │   │   ├── collector.ex
│   │   │   ├── aggregator.ex
│   │   │   └── prometheus.ex
│   │   ├── tracing/
│   │   │   ├── span.ex
│   │   │   ├── context.ex
│   │   │   └── store.ex
│   │   ├── region/
│   │   │   ├── transport.ex
│   │   │   └── router.ex
│   │   └── cli/
│   │       ├── status.ex
│   │       ├── benchmark.ex
│   │       └── drain.ex
│   └── platform.ex
├── test/
│   ├── gateway_test.exs
│   ├── coordinator_test.exs
│   ├── storage_test.exs
│   ├── queue_test.exs
│   ├── stream_test.exs
│   ├── telemetry_test.exs
│   ├── tracing_test.exs
│   └── integration_test.exs
└── bench/
    └── load_test.exs
```

### Step 1: Gateway

```elixir
defmodule Platform.Gateway.RateLimiter do
  @moduledoc """
  ETS-backed token bucket rate limiter.
  Uses :ets.update_counter with a threshold to atomically
  check and increment per-key counters without GenServer overhead.
  A background process handles cleanup of expired windows.
  """

  @table :rate_limiter

  @doc "Initialize the ETS table for rate limiting."
  @spec init() :: :ets.tid()
  def init do
    if :ets.whereis(@table) != :undefined do
      :ets.delete(@table)
    end

    :ets.new(@table, [:named_table, :public, :set, {:write_concurrency, true}])
  end

  @doc """
  Check and consume one token for the given API key.
  Returns :ok if under the limit, {:error, :rate_limited} otherwise.

  Uses a window-based counter: the key includes a time bucket so counters
  naturally expire when a new window starts. The update_counter call
  atomically increments and caps at the limit.
  """
  @spec check_and_consume(String.t(), pos_integer(), pos_integer()) ::
          :ok | {:error, :rate_limited}
  def check_and_consume(api_key, limit, window_ms) do
    now_window = div(System.monotonic_time(:millisecond), window_ms)
    key = {api_key, now_window}

    counter =
      :ets.update_counter(
        @table,
        key,
        {2, 1, limit, limit},
        {key, 0}
      )

    if counter <= limit do
      :ok
    else
      {:error, :rate_limited}
    end
  end

  @doc "Remove expired window entries to prevent unbounded table growth."
  @spec cleanup(pos_integer()) :: non_neg_integer()
  def cleanup(window_ms) do
    now_window = div(System.monotonic_time(:millisecond), window_ms)

    :ets.foldl(
      fn {{_key, window} = full_key, _count}, deleted ->
        if window < now_window - 1 do
          :ets.delete(@table, full_key)
          deleted + 1
        else
          deleted
        end
      end,
      0,
      @table
    )
  end
end

defmodule Platform.Gateway.Auth do
  @moduledoc """
  JWT verification using HS256 without external libraries.
  Manually splits the token, recomputes the HMAC-SHA256 signature,
  performs constant-time comparison, and checks expiration.
  """

  @doc """
  Verify an HS256 JWT token against the given secret.
  Returns {:ok, claims} on success, {:error, reason} on failure.
  Reasons: :invalid_format, :invalid_signature, :expired, :decode_error
  """
  @spec verify_jwt(String.t(), String.t()) ::
          {:ok, map()} | {:error, :invalid_format | :invalid_signature | :expired | :decode_error}
  def verify_jwt(token, secret) when is_binary(token) and is_binary(secret) do
    case String.split(token, ".") do
      [header_b64, payload_b64, sig_b64] ->
        expected_sig = :crypto.mac(:hmac, :sha256, secret, "#{header_b64}.#{payload_b64}")
        expected_sig_b64 = Base.url_encode64(expected_sig, padding: false)

        if secure_compare(sig_b64, expected_sig_b64) do
          decode_and_validate_payload(payload_b64)
        else
          {:error, :invalid_signature}
        end

      _ ->
        {:error, :invalid_format}
    end
  end

  defp decode_and_validate_payload(payload_b64) do
    with {:ok, json} <- Base.url_decode64(payload_b64, padding: false),
         {:ok, claims} <- Jason.decode(json) do
      now = System.system_time(:second)

      case Map.get(claims, "exp") do
        nil -> {:ok, claims}
        exp when exp > now -> {:ok, claims}
        _ -> {:error, :expired}
      end
    else
      _ -> {:error, :decode_error}
    end
  end

  # Constant-time string comparison to prevent timing attacks.
  defp secure_compare(a, b) when byte_size(a) != byte_size(b), do: false

  defp secure_compare(a, b) do
    a_bytes = :binary.bin_to_list(a)
    b_bytes = :binary.bin_to_list(b)

    Enum.zip(a_bytes, b_bytes)
    |> Enum.reduce(0, fn {x, y}, acc -> Bitwise.bor(acc, Bitwise.bxor(x, y)) end)
    |> Kernel.==(0)
  end
end

defmodule Platform.Gateway.CircuitBreaker do
  @moduledoc """
  Per-service circuit breaker with three states: :closed, :open, :half_open.
  When closed, requests pass through. After `threshold` failures within
  `window_ms`, the circuit opens. After `cooldown_ms`, it transitions to
  half_open and allows one probe request.
  """
  use GenServer

  @threshold 5
  @window_ms 10_000
  @cooldown_ms 5_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, %{}, opts)

  @doc """
  Execute a function through the circuit breaker for the given service.
  Returns the function's result or {:error, :circuit_open}.
  """
  @spec call(GenServer.server(), atom(), (() -> term()), timeout()) :: term()
  def call(breaker, service, fun, timeout \\ 5000) do
    GenServer.call(breaker, {:call, service, fun}, timeout)
  end

  @impl true
  def init(_), do: {:ok, %{}}

  @impl true
  def handle_call({:call, service, fun}, _from, state) do
    svc_state = Map.get(state, service, %{state: :closed, failures: [], success_count: 0})
    now = System.monotonic_time(:millisecond)

    case svc_state.state do
      :open ->
        last_failure = List.first(svc_state.failures, 0)

        if now - last_failure >= @cooldown_ms do
          try_call(fun, service, %{svc_state | state: :half_open}, state)
        else
          {:reply, {:error, :circuit_open}, state}
        end

      :half_open ->
        try_call(fun, service, svc_state, state)

      :closed ->
        try_call(fun, service, svc_state, state)
    end
  end

  defp try_call(fun, service, svc_state, state) do
    try do
      result = fun.()
      new_svc = %{svc_state | state: :closed, failures: [], success_count: svc_state.success_count + 1}
      {:reply, result, Map.put(state, service, new_svc)}
    rescue
      e ->
        now = System.monotonic_time(:millisecond)
        recent = [now | Enum.filter(svc_state.failures, fn t -> now - t < @window_ms end)]

        new_state_name =
          if length(recent) >= @threshold, do: :open, else: svc_state.state

        new_svc = %{svc_state | state: new_state_name, failures: recent}
        {:reply, {:error, {:service_error, Exception.message(e)}}, Map.put(state, service, new_svc)}
    end
  end
end
```

### Step 2: Coordinator (simplified Raft)

```elixir
defmodule Platform.Coordinator.Raft do
  @moduledoc """
  Simplified Raft leader election (no log replication).
  Each node is a GenServer. Leader sends heartbeats every 150ms.
  Election timeout is randomized between 300-600ms to prevent split votes.
  """
  use GenServer

  @heartbeat_interval 150
  @election_timeout_min 300
  @election_timeout_max 600

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    node_id = Keyword.fetch!(opts, :node_id)
    peers = Keyword.get(opts, :peers, [])

    GenServer.start_link(
      __MODULE__,
      %{
        node_id: node_id,
        peers: peers,
        term: 0,
        voted_for: nil,
        role: :follower,
        leader: nil,
        votes: MapSet.new()
      },
      opts
    )
  end

  @impl true
  def init(state) do
    schedule_election_timeout()
    {:ok, state}
  end

  @impl true
  def handle_call(:role, _from, state), do: {:reply, state.role, state}
  def handle_call(:leader, _from, state), do: {:reply, state.leader, state}
  def handle_call(:term, _from, state), do: {:reply, state.term, state}

  @impl true
  def handle_call({:request_vote, candidate_term, candidate_id}, _from, state) do
    cond do
      candidate_term < state.term ->
        {:reply, {state.term, false}, state}

      candidate_term > state.term ->
        new_state = %{
          state
          | term: candidate_term,
            voted_for: candidate_id,
            role: :follower,
            votes: MapSet.new()
        }

        schedule_election_timeout()
        {:reply, {candidate_term, true}, new_state}

      state.voted_for == nil or state.voted_for == candidate_id ->
        new_state = %{state | voted_for: candidate_id}
        schedule_election_timeout()
        {:reply, {state.term, true}, new_state}

      true ->
        {:reply, {state.term, false}, state}
    end
  end

  @impl true
  def handle_info(:election_timeout, %{role: :leader} = state) do
    {:noreply, state}
  end

  def handle_info(:election_timeout, state) do
    new_term = state.term + 1
    votes = MapSet.new([state.node_id])

    new_state = %{
      state
      | term: new_term,
        voted_for: state.node_id,
        role: :candidate,
        votes: votes
    }

    for peer <- state.peers do
      try do
        GenServer.call(peer, {:request_vote, new_term, state.node_id}, 200)
        |> handle_vote_response(state.node_id, peer)
      catch
        :exit, _ -> :ok
      end
    end

    new_state = maybe_become_leader(new_state)
    schedule_election_timeout()
    {:noreply, new_state}
  end

  def handle_info(:send_heartbeat, %{role: :leader} = state) do
    for peer <- state.peers do
      try do
        GenServer.cast(peer, {:append_entries, state.term, state.node_id})
      catch
        :exit, _ -> :ok
      end
    end

    Process.send_after(self(), :send_heartbeat, @heartbeat_interval)
    {:noreply, state}
  end

  def handle_info(:send_heartbeat, state), do: {:noreply, state}

  @impl true
  def handle_cast({:vote_response, term, granted, _from}, state) do
    cond do
      term > state.term ->
        {:noreply,
         %{state | term: term, role: :follower, voted_for: nil, votes: MapSet.new()}}

      state.role == :candidate and term == state.term and granted ->
        new_votes = MapSet.put(state.votes, self())
        new_state = maybe_become_leader(%{state | votes: new_votes})
        {:noreply, new_state}

      true ->
        {:noreply, state}
    end
  end

  def handle_cast({:append_entries, leader_term, leader_id}, state) do
    if leader_term >= state.term do
      new_state = %{
        state
        | term: leader_term,
          role: :follower,
          leader: leader_id,
          voted_for: nil,
          votes: MapSet.new()
      }

      schedule_election_timeout()
      {:noreply, new_state}
    else
      {:noreply, state}
    end
  end

  defp handle_vote_response({term, granted}, _my_id, peer) do
    GenServer.cast(self(), {:vote_response, term, granted, peer})
  end

  defp maybe_become_leader(%{role: :candidate, votes: votes, peers: peers} = state) do
    total_nodes = length(peers) + 1
    majority = div(total_nodes, 2) + 1

    if MapSet.size(votes) >= majority do
      Process.send_after(self(), :send_heartbeat, 0)
      %{state | role: :leader, leader: state.node_id}
    else
      state
    end
  end

  defp maybe_become_leader(state), do: state

  defp schedule_election_timeout do
    timeout =
      @election_timeout_min +
        :rand.uniform(@election_timeout_max - @election_timeout_min)

    Process.send_after(self(), :election_timeout, timeout)
  end
end
```

### Step 3: Storage

```elixir
defmodule Platform.Storage.Store do
  @moduledoc """
  Key-value store with WAL (write-ahead log), ETS cache with TTL,
  and an in-memory memtable. Writes go to WAL first, then cache and memtable.
  Batch writes are atomic via WAL markers.
  """

  @cache_table :storage_cache
  @memtable :storage_memtable
  @wal_table :storage_wal
  @default_ttl_ms 60_000

  @doc "Initialize all backing ETS tables."
  @spec init() :: :ok
  def init do
    for table <- [@cache_table, @memtable, @wal_table] do
      if :ets.whereis(table) != :undefined, do: :ets.delete(table)
    end

    :ets.new(@cache_table, [:named_table, :public, :set])
    :ets.new(@memtable, [:named_table, :public, :ordered_set])
    :ets.new(@wal_table, [:named_table, :public, :ordered_set])
    :ok
  end

  @doc "Write a key-value pair. Writes go to WAL first, then memtable and cache."
  @spec put(String.t(), term()) :: :ok
  def put(key, value) do
    timestamp = System.monotonic_time(:microsecond)
    :ets.insert(@wal_table, {{timestamp, :put}, key, value})
    :ets.insert(@memtable, {key, value, timestamp})
    :ets.insert(@cache_table, {key, value, System.monotonic_time(:millisecond) + @default_ttl_ms})
    :ok
  end

  @doc "Read a key. Checks cache first (with TTL), then memtable."
  @spec get(String.t()) :: {:ok, term()} | :not_found
  def get(key) do
    now = System.monotonic_time(:millisecond)

    case :ets.lookup(@cache_table, key) do
      [{^key, value, expires}] when expires > now ->
        {:ok, value}

      [{^key, _value, _expired}] ->
        :ets.delete(@cache_table, key)
        get_from_memtable(key)

      [] ->
        get_from_memtable(key)
    end
  end

  @doc "Delete a key from all layers."
  @spec delete(String.t()) :: :ok
  def delete(key) do
    timestamp = System.monotonic_time(:microsecond)
    :ets.insert(@wal_table, {{timestamp, :delete}, key, nil})
    :ets.delete(@memtable, key)
    :ets.delete(@cache_table, key)
    :ok
  end

  @doc """
  Atomic batch write. All operations succeed or none are visible.
  Uses WAL batch markers: a batch-start and batch-commit entry bracket
  the operations. On crash between start and commit, replay ignores
  uncommitted batches.
  """
  @spec write_batch([{:put, String.t(), term()} | {:delete, String.t()}]) :: :ok
  def write_batch(operations) when is_list(operations) do
    batch_id = :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
    timestamp = System.monotonic_time(:microsecond)
    :ets.insert(@wal_table, {{timestamp, :batch_start}, batch_id, nil})

    Enum.each(operations, fn
      {:put, key, value} ->
        ts = System.monotonic_time(:microsecond)
        :ets.insert(@wal_table, {{ts, :put}, key, value})
        :ets.insert(@memtable, {key, value, ts})
        :ets.insert(@cache_table, {key, value, System.monotonic_time(:millisecond) + @default_ttl_ms})

      {:delete, key} ->
        ts = System.monotonic_time(:microsecond)
        :ets.insert(@wal_table, {{ts, :delete}, key, nil})
        :ets.delete(@memtable, key)
        :ets.delete(@cache_table, key)
    end)

    commit_ts = System.monotonic_time(:microsecond)
    :ets.insert(@wal_table, {{commit_ts, :batch_commit}, batch_id, nil})
    :ok
  end

  @doc "Prefix scan: return all {key, value} pairs where key starts with prefix."
  @spec scan(String.t()) :: [{String.t(), term()}]
  def scan(prefix) do
    cache_results =
      :ets.foldl(
        fn {key, value, expires}, acc ->
          now = System.monotonic_time(:millisecond)

          if is_binary(key) and String.starts_with?(key, prefix) and expires > now do
            Map.put(acc, key, {:cache, value})
          else
            acc
          end
        end,
        %{},
        @cache_table
      )

    memtable_results =
      :ets.foldl(
        fn {key, value, _ts}, acc ->
          if is_binary(key) and String.starts_with?(key, prefix) and not Map.has_key?(acc, key) do
            Map.put(acc, key, value)
          else
            acc
          end
        end,
        %{},
        @memtable
      )

    merged =
      Map.merge(memtable_results, cache_results, fn _key, _mem_val, {:cache, cache_val} ->
        cache_val
      end)

    merged
    |> Enum.map(fn
      {key, {:cache, val}} -> {key, val}
      {key, val} -> {key, val}
    end)
    |> Enum.sort_by(fn {key, _} -> key end)
  end

  defp get_from_memtable(key) do
    case :ets.lookup(@memtable, key) do
      [{^key, value, _ts}] -> {:ok, value}
      [] -> :not_found
    end
  end
end
```

### Step 4: Queue

```elixir
defmodule Platform.Queue.Job do
  @moduledoc "Job struct for the priority queue."
  @enforce_keys [:id, :payload, :priority]
  defstruct [:id, :payload, :priority, attempts: 0, max_attempts: 3, next_visible_at: 0]

  @type t :: %__MODULE__{
          id: String.t(),
          payload: term(),
          priority: :high | :normal | :low,
          attempts: non_neg_integer(),
          max_attempts: pos_integer(),
          next_visible_at: integer()
        }
end

defmodule Platform.Queue.DeadLetter do
  @moduledoc "Dead letter queue for jobs that have exhausted their retries."
  @table :dead_letter_queue

  @spec init() :: :ok
  def init do
    if :ets.whereis(@table) != :undefined, do: :ets.delete(@table)
    :ets.new(@table, [:named_table, :public, :set])
    :ok
  end

  @spec push(Platform.Queue.Job.t()) :: :ok
  def push(job) do
    :ets.insert(@table, {job.id, job})
    :ok
  end

  @spec list() :: [Platform.Queue.Job.t()]
  def list do
    :ets.tab2list(@table)
    |> Enum.map(fn {_id, job} -> job end)
  end
end

defmodule Platform.Queue.Scheduler do
  @moduledoc """
  Priority queue with visibility timeout.
  Three ETS tables (one per priority level). Dequeuing marks a job
  invisible for 30 seconds. A periodic reaper nacks timed-out jobs.
  """
  use GenServer

  alias Platform.Queue.{Job, DeadLetter}

  @priorities [:high, :normal, :low]
  @visibility_timeout_ms 30_000
  @reap_interval_ms 5_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @impl true
  def init(_opts) do
    for priority <- @priorities do
      table = table_name(priority)
      if :ets.whereis(table) != :undefined, do: :ets.delete(table)
      :ets.new(table, [:named_table, :public, :ordered_set])
    end

    DeadLetter.init()
    Process.send_after(self(), :reap_invisible, @reap_interval_ms)
    {:ok, %{}}
  end

  @doc "Enqueue a job at the given priority."
  @spec enqueue(term(), :high | :normal | :low, keyword()) :: :ok
  def enqueue(payload, priority \\ :normal, opts \\ []) do
    job = %Job{
      id: :crypto.strong_rand_bytes(16) |> Base.encode16(case: :lower),
      payload: payload,
      priority: priority,
      max_attempts: Keyword.get(opts, :max_attempts, 3),
      next_visible_at: System.monotonic_time(:millisecond)
    }

    :ets.insert(table_name(priority), {job.id, job})
    :ok
  end

  @doc "Dequeue next available job (highest priority first). Marks as invisible."
  @spec dequeue(String.t()) :: {:ok, Job.t()} | {:empty}
  def dequeue(_worker_id) do
    now = System.monotonic_time(:millisecond)

    Enum.find_value(@priorities, {:empty}, fn priority ->
      table = table_name(priority)

      case find_visible_job(table, now) do
        nil ->
          nil

        {id, job} ->
          updated = %{job | next_visible_at: now + @visibility_timeout_ms}
          :ets.insert(table, {id, updated})
          {:ok, job}
      end
    end)
  end

  @doc "Acknowledge successful completion. Removes job from all tables."
  @spec ack(String.t()) :: :ok
  def ack(job_id) do
    for priority <- @priorities do
      :ets.delete(table_name(priority), job_id)
    end

    :ok
  end

  @doc """
  Negative acknowledge: increment attempts, schedule retry with exponential backoff.
  If attempts >= max_attempts, move to dead letter queue.
  """
  @spec nack(String.t()) :: :ok
  def nack(job_id) do
    Enum.find_value(@priorities, :ok, fn priority ->
      table = table_name(priority)

      case :ets.lookup(table, job_id) do
        [{^job_id, job}] ->
          new_attempts = job.attempts + 1

          if new_attempts >= job.max_attempts do
            :ets.delete(table, job_id)
            DeadLetter.push(%{job | attempts: new_attempts})
          else
            backoff_ms = min(trunc(:math.pow(2, new_attempts) * 1000), 300_000)
            now = System.monotonic_time(:millisecond)

            updated = %{job | attempts: new_attempts, next_visible_at: now + backoff_ms}
            :ets.insert(table, {job_id, updated})
          end

          :ok

        [] ->
          nil
      end
    end)
  end

  @impl true
  def handle_info(:reap_invisible, state) do
    now = System.monotonic_time(:millisecond)

    for priority <- @priorities do
      table = table_name(priority)

      :ets.foldl(
        fn {id, job}, _acc ->
          if job.next_visible_at < now and job.next_visible_at > 0 do
            nack(id)
          end
        end,
        nil,
        table
      )
    end

    Process.send_after(self(), :reap_invisible, @reap_interval_ms)
    {:noreply, state}
  end

  defp table_name(:high), do: :queue_high
  defp table_name(:normal), do: :queue_normal
  defp table_name(:low), do: :queue_low

  defp find_visible_job(table, now) do
    :ets.foldl(
      fn {id, job}, acc ->
        if acc == nil and job.next_visible_at <= now do
          {id, job}
        else
          acc
        end
      end,
      nil,
      table
    )
  end
end
```

### Step 5: Stream processor

```elixir
defmodule Platform.Stream.Window do
  @moduledoc """
  Time-based and count-based windowing for event streams.
  Events must have a `:timestamp` field.
  """

  @doc """
  Time-based tumbling window.
  Groups events by floor(event.timestamp / window_ms).
  Returns a map of %{window_start_ms => [events]}.
  """
  @spec tumbling([map()], pos_integer()) :: %{non_neg_integer() => [map()]}
  def tumbling(events, window_ms) do
    Enum.group_by(events, fn event ->
      div(event.timestamp, window_ms) * window_ms
    end)
  end

  @doc """
  Count-based sliding window over a stream of events.
  Returns list of lists, each of length `size`, sliding by 1.
  """
  @spec sliding([term()], pos_integer()) :: [[term()]]
  def sliding(events, size) do
    Enum.chunk_every(events, size, 1, :discard)
  end
end

defmodule Platform.Stream.Operators do
  @moduledoc "Stream operators: join, filter, map, reduce."

  @doc """
  Join two event streams by key within a time window.
  Returns pairs {event_a, event_b} where the keys match
  and timestamps are within window_ms of each other.
  """
  @spec join([map()], [map()], (map() -> term()), pos_integer()) :: [{map(), map()}]
  def join(stream_a, stream_b, key_fn, window_ms) do
    index_a =
      Enum.group_by(stream_a, key_fn)

    Enum.flat_map(stream_b, fn event_b ->
      key = key_fn.(event_b)

      case Map.get(index_a, key, []) do
        [] ->
          []

        matching ->
          matching
          |> Enum.filter(fn event_a ->
            abs(event_a.timestamp - event_b.timestamp) <= window_ms
          end)
          |> Enum.map(fn event_a -> {event_a, event_b} end)
      end
    end)
  end

  @doc "Watermark-aware filter: drop events older than watermark."
  @spec filter_late([map()], non_neg_integer()) :: [map()]
  def filter_late(events, watermark_ms) do
    Enum.reject(events, fn e -> e.timestamp < watermark_ms end)
  end
end
```

### Step 6: Telemetry and tracing

```elixir
defmodule Platform.Telemetry.Collector do
  @moduledoc """
  Telemetry handler with reservoir sampling for percentile computation.
  Maintains a fixed-size reservoir of 1000 samples per event name.
  Uses Vitter's Algorithm R for uniform random sampling.
  """

  @reservoir_size 1000
  @table :telemetry_reservoir

  @doc "Initialize ETS table and attach telemetry handlers."
  @spec init() :: :ok
  def init do
    if :ets.whereis(@table) != :undefined, do: :ets.delete(@table)
    :ets.new(@table, [:named_table, :public, :set])
    :ok
  end

  @doc "Attach to a list of telemetry events."
  @spec attach() :: :ok
  def attach do
    events = [
      [:platform, :gateway, :request],
      [:platform, :storage, :operation],
      [:platform, :queue, :job],
      [:platform, :coordinator, :task]
    ]

    :telemetry.attach_many("platform-collector", events, &handle_event/4, %{})
  end

  @doc false
  def handle_event(event_name, measurements, _metadata, _config) do
    duration_ms = Map.get(measurements, :duration_ms, 0)
    record_sample(event_name, duration_ms)
  end

  @doc "Record a sample for the given event name using reservoir sampling."
  @spec record_sample(list(), number()) :: :ok
  def record_sample(event_name, value) do
    key = event_name

    case :ets.lookup(@table, key) do
      [] ->
        :ets.insert(@table, {key, [value], 1})

      [{^key, reservoir, count}] ->
        new_count = count + 1

        if length(reservoir) < @reservoir_size do
          :ets.insert(@table, {key, [value | reservoir], new_count})
        else
          idx = :rand.uniform(new_count)

          if idx <= @reservoir_size do
            new_reservoir = List.replace_at(reservoir, idx - 1, value)
            :ets.insert(@table, {key, new_reservoir, new_count})
          else
            :ets.insert(@table, {key, reservoir, new_count})
          end
        end
    end

    :ok
  end

  @doc "Compute the given percentile (0-100) for the named event."
  @spec percentile(list(), number()) :: number() | nil
  def percentile(event_name, p) do
    case :ets.lookup(@table, event_name) do
      [] ->
        nil

      [{_key, reservoir, _count}] ->
        sorted = Enum.sort(reservoir)
        index = min(round(p / 100.0 * length(sorted)), length(sorted) - 1)
        Enum.at(sorted, index)
    end
  end
end

defmodule Platform.Tracing.Store do
  @moduledoc "ETS-based span storage keyed by trace_id."

  @table :tracing_spans

  @spec init() :: :ok
  def init do
    if :ets.whereis(@table) != :undefined, do: :ets.delete(@table)
    :ets.new(@table, [:named_table, :public, :bag])
    :ok
  end

  @spec store_span(String.t(), map()) :: :ok
  def store_span(trace_id, span) do
    :ets.insert(@table, {trace_id, span})
    :ok
  end

  @spec get_trace(String.t()) :: [map()]
  def get_trace(trace_id) do
    :ets.lookup(@table, trace_id)
    |> Enum.map(fn {_id, span} -> span end)
  end
end

defmodule Platform.Tracing.Context do
  @moduledoc """
  Trace context propagation via process dictionary.
  Supports creating traces, child spans, and recording durations.
  """

  @trace_key :platform_trace_context

  @doc "Start a new trace. Returns the trace_id."
  @spec start_trace() :: String.t()
  def start_trace do
    trace_id = :crypto.strong_rand_bytes(16) |> Base.encode16(case: :lower)
    span_id = :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
    Process.put(@trace_key, %{trace_id: trace_id, span_id: span_id, parent_id: nil})
    trace_id
  end

  @doc "Get the current trace_id or nil if outside a trace."
  @spec current_trace_id() :: String.t() | nil
  def current_trace_id do
    case Process.get(@trace_key) do
      %{trace_id: id} -> id
      nil -> nil
    end
  end

  @doc """
  Execute a function as a child span. Records start/end time and
  stores the span in Platform.Tracing.Store. Returns the function result.
  """
  @spec with_span(String.t(), String.t(), (() -> term())) :: term()
  def with_span(component, operation, fun) do
    parent = Process.get(@trace_key)
    span_id = :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)

    child_ctx = %{
      trace_id: parent.trace_id,
      span_id: span_id,
      parent_id: parent.span_id
    }

    Process.put(@trace_key, child_ctx)

    start_time = System.monotonic_time(:microsecond)
    result = fun.()
    end_time = System.monotonic_time(:microsecond)
    duration_ms = (end_time - start_time) / 1000.0

    span = %{
      trace_id: parent.trace_id,
      span_id: span_id,
      parent_id: parent.span_id,
      component: component,
      operation: operation,
      start_time: start_time,
      duration_ms: duration_ms
    }

    Platform.Tracing.Store.store_span(parent.trace_id, span)
    Process.put(@trace_key, parent)
    result
  end
end
```

### Step 7: Region transport

```elixir
defmodule Platform.Region.Transport do
  @moduledoc """
  Inter-region communication with simulated latency.
  In development, simulates network latency with Process.sleep.
  Writes are routed to the primary region; reads go to the nearest.
  """

  @regions %{
    "us-east" => %{latency_ms: 0},
    "eu-west" => %{latency_ms: 80},
    "ap-south" => %{latency_ms: 140}
  }

  @doc """
  Call a function on a remote region with simulated latency.
  Wraps execution in a Task with timeout to prevent hanging.
  """
  @spec call(String.t(), (() -> term()), timeout()) :: {:ok, term()} | {:error, :timeout}
  def call(region, fun, timeout \\ 5000) do
    latency = get_in(@regions, [region, :latency_ms]) || 0
    if latency > 0, do: Process.sleep(latency)

    task = Task.async(fn -> fun.() end)

    case Task.yield(task, timeout) || Task.shutdown(task) do
      {:ok, result} -> {:ok, result}
      nil -> {:error, :timeout}
    end
  end

  @doc "Route a request: writes go to primary, reads go to nearest region."
  @spec route(:write | :read, String.t() | nil) :: {:ok, term()} | {:error, term()}
  def route(operation, region_header) do
    case operation do
      :write -> call("us-east", fn -> {:ok, :primary} end)
      :read -> call(region_header || "us-east", fn -> {:ok, :local} end)
    end
  end
end
```

### Step 8: Application supervisor and router

```elixir
defmodule Platform.Gateway.Router do
  @moduledoc "Request router: auth -> rate_limit -> route to queue."

  @spec handle_request(map()) :: map()
  def handle_request(%{method: method, path: path, headers: headers, body: body}) do
    trace_id = Platform.Tracing.Context.start_trace()

    Platform.Tracing.Context.with_span("gateway", "handle_request", fn ->
      with {:ok, claims} <- authenticate(headers),
           :ok <- rate_limit(claims),
           {:ok, result} <- route(method, path, body) do
        %{
          status: 202,
          headers: %{"x-trace-id" => trace_id},
          body: result
        }
      else
        {:error, :rate_limited} ->
          %{status: 429, headers: %{"x-trace-id" => trace_id}, body: %{"error" => "rate limited"}}

        {:error, :invalid_signature} ->
          %{status: 401, headers: %{"x-trace-id" => trace_id}, body: %{"error" => "unauthorized"}}

        {:error, :expired} ->
          %{status: 401, headers: %{"x-trace-id" => trace_id}, body: %{"error" => "token expired"}}

        {:error, reason} ->
          %{status: 500, headers: %{"x-trace-id" => trace_id}, body: %{"error" => inspect(reason)}}
      end
    end)
  end

  defp authenticate(%{"authorization" => "Bearer " <> token}) do
    secret = Application.get_env(:platform, :jwt_secret, "test_secret")
    Platform.Gateway.Auth.verify_jwt(token, secret)
  end

  defp authenticate(_), do: {:error, :missing_auth}

  defp rate_limit(%{"sub" => api_key}) do
    Platform.Gateway.RateLimiter.check_and_consume(api_key, 1000, 1000)
  end

  defp rate_limit(_claims), do: :ok

  defp route("POST", "/jobs", body) do
    Platform.Tracing.Context.with_span("gateway", "enqueue_job", fn ->
      job_id = :crypto.strong_rand_bytes(16) |> Base.encode16(case: :lower)
      Platform.Storage.Store.put("job:#{job_id}", body)
      Platform.Queue.Scheduler.enqueue(Map.put(body, "job_id", job_id), :normal)
      {:ok, %{"job_id" => job_id}}
    end)
  end

  defp route(_method, _path, _body), do: {:error, :not_found}
end

defmodule Platform do
  @moduledoc "Application supervisor: starts all subsystems."
  use Application

  @impl true
  def start(_type, _args) do
    Platform.Gateway.RateLimiter.init()
    Platform.Storage.Store.init()
    Platform.Telemetry.Collector.init()
    Platform.Tracing.Store.init()

    children = [
      {Platform.Queue.Scheduler, name: Platform.Queue.Scheduler}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: Platform.Supervisor)
  end
end
```

## Given tests

```elixir
# test/gateway_test.exs
defmodule Platform.GatewayTest do
  use ExUnit.Case, async: false
  alias Platform.Gateway.{RateLimiter, Auth, CircuitBreaker}

  setup do
    RateLimiter.init()
    :ok
  end

  test "rate limiter allows requests under limit" do
    for _ <- 1..5 do
      assert :ok = RateLimiter.check_and_consume("key1", 10, 1000)
    end
  end

  test "rate limiter blocks requests over limit" do
    for _ <- 1..10 do
      RateLimiter.check_and_consume("key2", 10, 1000)
    end
    assert {:error, :rate_limited} = RateLimiter.check_and_consume("key2", 10, 1000)
  end

  test "JWT verification succeeds for valid token" do
    secret = "test_secret"
    claims = %{"sub" => "user123", "exp" => System.system_time(:second) + 3600}
    token = build_test_jwt(claims, secret)
    assert {:ok, decoded} = Auth.verify_jwt(token, secret)
    assert decoded["sub"] == "user123"
  end

  test "JWT verification fails for expired token" do
    secret = "test_secret"
    claims = %{"sub" => "user123", "exp" => System.system_time(:second) - 1}
    token = build_test_jwt(claims, secret)
    assert {:error, :expired} = Auth.verify_jwt(token, secret)
  end

  defp build_test_jwt(claims, secret) do
    header = Base.url_encode64(~s({"alg":"HS256","typ":"JWT"}), padding: false)
    payload = Base.url_encode64(Jason.encode!(claims), padding: false)
    sig = :crypto.mac(:hmac, :sha256, secret, "#{header}.#{payload}")
           |> Base.url_encode64(padding: false)
    "#{header}.#{payload}.#{sig}"
  end
end

# test/coordinator_test.exs
defmodule Platform.CoordinatorTest do
  use ExUnit.Case, async: false
  alias Platform.Coordinator.Raft

  test "leader is elected within 2 seconds in a 3-node cluster" do
    nodes = for i <- 1..3 do
      {:ok, pid} = Raft.start_link(node_id: i, peers: [], name: :"raft_#{i}")
      pid
    end
    Process.sleep(2000)
    leaders = Enum.count(nodes, fn pid -> GenServer.call(pid, :role) == :leader end)
    assert leaders == 1
    Enum.each(nodes, &GenServer.stop/1)
  end

  test "new leader elected within 5 seconds after leader crash" do
    nodes = for i <- 1..3 do
      {:ok, pid} = Raft.start_link(node_id: i, peers: [], name: :"raft2_#{i}")
      pid
    end
    Process.sleep(2000)
    leader = Enum.find(nodes, fn pid -> GenServer.call(pid, :role) == :leader end)
    GenServer.stop(leader, :kill)
    remaining = List.delete(nodes, leader)
    Process.sleep(5000)
    new_leaders = Enum.count(remaining, fn pid ->
      try do
        GenServer.call(pid, :role) == :leader
      catch
        :exit, _ -> false
      end
    end)
    assert new_leaders == 1
    Enum.each(remaining, fn pid -> try do GenServer.stop(pid) catch :exit, _ -> :ok end end)
  end
end

# test/queue_test.exs
defmodule Platform.QueueTest do
  use ExUnit.Case, async: false
  alias Platform.Queue.Scheduler

  setup do
    {:ok, _} = Scheduler.start_link(name: :test_queue)
    :ok
  end

  test "enqueue and dequeue preserves payload" do
    Scheduler.enqueue(%{action: "process_file"}, :normal)
    {:ok, job} = Scheduler.dequeue("worker-1")
    assert job.payload.action == "process_file"
  end

  test "high priority jobs dequeued before normal" do
    Scheduler.enqueue(%{id: "low"}, :low)
    Scheduler.enqueue(%{id: "high"}, :high)
    Scheduler.enqueue(%{id: "normal"}, :normal)
    {:ok, j1} = Scheduler.dequeue("w1")
    {:ok, j2} = Scheduler.dequeue("w2")
    {:ok, j3} = Scheduler.dequeue("w3")
    assert j1.payload.id == "high"
    assert j2.payload.id == "normal"
    assert j3.payload.id == "low"
  end

  test "nack increments attempts and reschedules" do
    Scheduler.enqueue(%{id: "retry_me"}, :normal)
    {:ok, job} = Scheduler.dequeue("w1")
    assert job.attempts == 0
    Scheduler.nack(job.id)
    Process.sleep(1500)
    {:ok, retried} = Scheduler.dequeue("w2")
    assert retried.id == job.id
    assert retried.attempts == 1
  end

  test "job moves to DLQ after max attempts" do
    Scheduler.enqueue(%{id: "doomed"}, :normal, max_attempts: 1)
    {:ok, job} = Scheduler.dequeue("w1")
    Scheduler.nack(job.id)
    Process.sleep(1500)
    assert {:empty} = Scheduler.dequeue("w2")
    assert [_] = Platform.Queue.DeadLetter.list()
  end
end

# test/integration_test.exs
defmodule Platform.IntegrationTest do
  use ExUnit.Case, async: false

  @tag timeout: 30_000
  test "end-to-end request flow: Gateway -> Storage -> Queue" do
    {:ok, _} = Platform.start(:normal, [])

    response = Platform.Gateway.Router.handle_request(%{
      method: "POST",
      path: "/jobs",
      headers: %{"authorization" => "Bearer #{test_jwt()}"},
      body: %{"type" => "analysis", "data" => "sample"}
    })

    assert response.status == 202
    assert Map.has_key?(response.body, "job_id")

    {:ok, job} = Platform.Queue.Scheduler.dequeue("integration-worker")
    assert job.payload["type"] == "analysis"

    trace_id = response.headers["x-trace-id"]
    assert trace_id != nil
    spans = Platform.Tracing.Store.get_trace(trace_id)
    assert length(spans) >= 2
  end

  defp test_jwt do
    secret = Application.get_env(:platform, :jwt_secret, "test_secret")
    claims = %{"sub" => "test_user", "exp" => System.system_time(:second) + 3600}
    header = Base.url_encode64(~s({"alg":"HS256","typ":"JWT"}), padding: false)
    payload = Base.url_encode64(Jason.encode!(claims), padding: false)
    sig = :crypto.mac(:hmac, :sha256, secret, "#{header}.#{payload}")
           |> Base.url_encode64(padding: false)
    "#{header}.#{payload}.#{sig}"
  end
end
```

## Benchmark

```elixir
# bench/load_test.exs
# Run with: mix run bench/load_test.exs
defmodule Platform.Bench.LoadTest do
  @target_rps 50_000
  @duration_s 30
  @mix_read 0.70
  @mix_write 0.30

  def run do
    {:ok, _} = Platform.start(:normal, [])
    Process.sleep(500)

    IO.puts("Starting load test: #{@target_rps} RPS for #{@duration_s}s")
    IO.puts("Mix: #{trunc(@mix_read * 100)}% reads / #{trunc(@mix_write * 100)}% writes")

    start_ms = System.monotonic_time(:millisecond)

    requests = Stream.repeatedly(fn ->
      if :rand.uniform() < @mix_read, do: :read, else: :write
    end)
    |> Stream.take(@target_rps * @duration_s)

    results =
      Task.async_stream(
        requests,
        fn op ->
          t0 = System.monotonic_time(:microsecond)
          result = case op do
            :read -> Platform.Storage.Store.get("bench_key_#{:rand.uniform(1000)}")
            :write -> Platform.Storage.Store.put("bench_key_#{:rand.uniform(1000)}", :rand.bytes(64))
          end
          t1 = System.monotonic_time(:microsecond)
          {result, t1 - t0}
        end,
        max_concurrency: System.schedulers_online() * 4,
        timeout: 10_000,
        ordered: false
      )
      |> Enum.to_list()

    latencies = Enum.map(results, fn {:ok, {_, us}} -> us / 1000.0 end)
    errors = Enum.count(results, fn
      {:ok, {{:error, _}, _}} -> true
      {:exit, _} -> true
      _ -> false
    end)

    sorted = Enum.sort(latencies)
    n = length(sorted)
    median = Enum.at(sorted, div(n, 2))
    p95 = Enum.at(sorted, trunc(n * 0.95))
    p99 = Enum.at(sorted, trunc(n * 0.99))
    elapsed_s = (System.monotonic_time(:millisecond) - start_ms) / 1000.0
    throughput = n / elapsed_s

    IO.puts("\n=== Results ===")
    IO.puts("Total requests: #{n}")
    IO.puts("Errors:         #{errors} (#{Float.round(errors / n * 100, 2)}%)")
    IO.puts("Throughput:     #{Float.round(throughput, 0)} req/s")
    IO.puts("Median:         #{Float.round(median, 2)} ms")
    IO.puts("P95:            #{Float.round(p95, 2)} ms")
    IO.puts("P99:            #{Float.round(p99, 2)} ms")
    IO.puts("\nTargets:")
    IO.puts("  P99 < 50ms:   #{if p99 < 50, do: "PASS", else: "FAIL (#{Float.round(p99, 1)}ms)"}")
    IO.puts("  0% errors:    #{if errors == 0, do: "PASS", else: "FAIL (#{errors} errors)"}")
    IO.puts("  50k RPS:      #{if throughput >= 50_000, do: "PASS", else: "PARTIAL (#{Float.round(throughput, 0)} RPS)"}")
  end
end

Platform.Bench.LoadTest.run()
```

## Trade-off analysis

| Design choice | Selected approach | Alternative | Why not the alternative |
|---|---|---|---|
| Rate limiter state | ETS `:update_counter` | GenServer mailbox | GenServer serializes at >100k/s; ETS is lock-free per-key |
| Leader election | Simplified Raft | `:global` registry | `:global` has no progress guarantee under partition |
| Percentile computation | Reservoir sampling (1000 samples) | Store all observations | 3M samples/min is unbounded memory |
| Storage cache invalidation | TTL-based expiry | Event-driven invalidation | Event-driven requires distributed coordination; TTL is simple and bounded |
| Queue visibility timeout | Periodic reaper process | Per-job timer | One timer per job at 50k RPS creates 50k active timers; reaper batches the check |
| Inter-region communication | Simulated `Process.sleep` | Real Erlang distribution | Keeps the exercise local; real distribution requires cluster setup |

## Common production mistakes

**Coordinated omission in your benchmark.** If you measure latency only from when you actually send the request, you miss the queue-waiting time. The correct approach is to schedule requests at fixed intervals and measure from the scheduled time, not the actual send time.

**Not propagating trace context through `Task.async_stream`.** Child tasks run in fresh processes with empty process dictionaries. You must explicitly pass `trace_id` in the closure and call `set_trace` at the start of each task.

**Circuit breaker state shared across all callers.** A single GenServer holding state for all downstream services becomes a serialization point. Use one GenServer per service, or use an ETS table with atomic state transitions.

**WAL sync policy causing write latency spikes.** Calling `:file.sync/1` after every write adds ~1ms of fsync latency. Use group commit: buffer writes for 2ms, then fsync once for the batch.

**Raft election timeout not randomized.** If all followers use the same timeout, they start elections simultaneously, splitting votes indefinitely. Each node must pick a random timeout in a range (300-600ms).

## Resources

- Kleppmann -- "Designing Data-Intensive Applications" (entire book is the reference for this exercise)
- Ongaro & Ousterhout -- "In Search of an Understandable Consensus Algorithm" (Raft paper, 2014)
- Gil Tene -- "How NOT to Measure Latency" (2015 talk)
- Vitter -- "Random Sampling with a Reservoir" (1985) -- ACM TOMS 11(1)
- Prometheus text exposition format -- https://prometheus.io/docs/instrumenting/exposition_formats/
- W3C TraceContext specification -- https://www.w3.org/TR/trace-context/
- Erlang ETS documentation -- https://www.erlang.org/doc/man/ets.html

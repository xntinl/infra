# Build an Event Bus (Capstone)

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` handles payment, signup, and analytics events from multiple upstream
services. These events must fan out to independent consumers (billing, notifications,
audit, metrics) without coupling producers to consumers. The gateway needs an internal
event bus: publish-subscribe with wildcard topic matching, per-topic event history,
dead-letter queue for failed handlers, and real-time metrics.

This capstone combines GenServer, ETS, `:queue`, supervised trees, and the testing
patterns from the advanced level into a single cohesive system.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── event_bus/
│           ├── server.ex            # ← you implement (Exercise 1)
│           ├── topic_matcher.ex     # ← you implement (Exercise 2)
│           ├── dlq_worker.ex        # ← you implement (Exercise 3)
│           ├── metrics_sampler.ex   # ← you implement (Exercise 4)
│           └── supervisor.ex        # ← you implement (Exercise 4)
├── test/
│   └── api_gateway/
│       └── event_bus/
│           ├── server_test.exs          # given tests
│           ├── topic_matcher_test.exs   # given tests
│           ├── dlq_test.exs             # given tests
│           └── metrics_test.exs         # given tests
└── mix.exs
```

---

## Architecture

```
┌──────────────────────────────────────────────────────┐
│              EventBus.Server (GenServer)              │
│                                                       │
│  subscriptions: %{compiled_pattern => [{ref, fn}]}   │
│  history:       %{topic => :queue.queue()}            │
│  dlq:           :queue.queue()                        │
│  metrics:       %{total, failed, rate, topics}        │
└──────────┬────────────────────────┬──────────────────┘
           │ publish                │ subscribe
    ┌──────▼──────┐          ┌──────▼──────┐
    │  Producer   │          │  Consumer   │
    │  (any proc) │          │  (handler)  │
    └─────────────┘          └─────────────┘
           │ wildcard match via TopicMatcher
    ┌──────▼──────────────────────────────────┐
    │  TopicMatcher                            │
    │  "orders.*" matches "orders.created"    │
    └──────────────┬──────────────────────────┘
                   │ on handler exception
    ┌──────────────▼──────────────┐
    │  Dead Letter Queue (DLQ)    │
    │  DLQWorker retries every 30s│
    └─────────────────────────────┘
```

---

## Exercise 1: Core PubSub

### Step 1: `lib/api_gateway/event_bus/server.ex`

```elixir
defmodule ApiGateway.EventBus.Server do
  @moduledoc """
  Core GenServer for the event bus.

  Responsibilities:
    1. Maintain subscriptions: pattern → [{ref, handler_fn}]
    2. Publish events: find matching handlers, dispatch via Task.start
    3. Monitor subscriber processes; clean up on :DOWN
    4. Maintain per-topic history (capped at max_history entries)
    5. Track metrics (events_total, failed_handlers)
    6. Route failed handler output to the DLQ

  The GenServer never crashes on handler failure — all dispatch is wrapped
  in a Task and failures are caught and recorded.
  """
  use GenServer

  alias ApiGateway.EventBus.TopicMatcher

  @max_history 100

  defstruct [
    subscriptions: %{},  # compiled_pattern (list) => [{ref, handler_fn}]
    monitors:      %{},  # pid => [compiled_pattern, ...]
    history:       %{},  # topic (string) => :queue.queue()
    dlq:           :queue.new(),
    metrics:       %{events_total: 0, failed_handlers: 0, rate: 0, topics: %{}}
  ]

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
  end

  @doc "Subscribe a handler function to a topic pattern. Returns a ref for unsubscribing."
  @spec subscribe(String.t(), (map() -> any()), GenServer.server()) :: reference()
  def subscribe(pattern, handler_fn, server \\ __MODULE__) do
    GenServer.call(server, {:subscribe, pattern, handler_fn})
  end

  @doc "Remove a subscription by ref."
  @spec unsubscribe(reference(), GenServer.server()) :: :ok
  def unsubscribe(ref, server \\ __MODULE__) do
    GenServer.cast(server, {:unsubscribe, ref})
  end

  @doc "Publish an event to a topic. Returns immediately — dispatch is async."
  @spec publish(String.t(), map(), GenServer.server()) :: :ok
  def publish(topic, event, server \\ __MODULE__) do
    GenServer.cast(server, {:publish, topic, event})
  end

  @doc "Retrieve recent events for a topic (or wildcard pattern)."
  @spec history(String.t(), keyword(), GenServer.server()) :: [map()]
  def history(pattern, opts \\ [], server \\ __MODULE__) do
    limit = Keyword.get(opts, :limit, 10)
    GenServer.call(server, {:history, pattern, limit})
  end

  @doc "Return dead-letter queue contents."
  @spec dlq_list(GenServer.server()) :: list()
  def dlq_list(server \\ __MODULE__) do
    GenServer.call(server, :dlq_list)
  end

  @doc "Retry all DLQ entries. Returns {:ok, %{retried: N, still_failed: M}}."
  @spec dlq_retry_all(GenServer.server()) :: {:ok, map()}
  def dlq_retry_all(server \\ __MODULE__) do
    GenServer.call(server, :dlq_retry_all)
  end

  @doc "Remove all DLQ entries."
  @spec dlq_purge(GenServer.server()) :: :ok
  def dlq_purge(server \\ __MODULE__) do
    GenServer.cast(server, :dlq_purge)
  end

  @doc "Return current metrics."
  @spec metrics(GenServer.server()) :: map()
  def metrics(server \\ __MODULE__) do
    GenServer.call(server, :metrics)
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(:ok) do
    {:ok, %__MODULE__{}}
  end

  @impl true
  def handle_call({:subscribe, pattern, handler_fn}, {caller_pid, _}, state) do
    ref             = make_ref()
    compiled        = TopicMatcher.compile(pattern)

    # TODO: add {ref, handler_fn} to state.subscriptions[compiled]
    # TODO: monitor caller_pid if not already monitored (store in state.monitors)
    # HINT: Process.monitor(caller_pid) returns a monitor reference
    # return {:reply, ref, new_state}
    {:reply, ref, state}
  end

  @impl true
  def handle_call({:history, pattern, limit}, _from, state) do
    compiled = TopicMatcher.compile(pattern)

    # TODO: collect events from all topics whose name matches the pattern
    # HINT: state.history is %{topic_string => :queue.queue()}
    # For each {topic, queue} in history, check TopicMatcher.matches?(compiled, topic)
    # Convert matching queues to lists, flatten, sort by :published_at, take last `limit`
    {:reply, [], state}
  end

  @impl true
  def handle_call(:dlq_list, _from, state) do
    entries = :queue.to_list(state.dlq)
    {:reply, entries, state}
  end

  @impl true
  def handle_call(:dlq_retry_all, _from, state) do
    # TODO: for each DLQ entry, call the original handler with the event
    # On success: remove from DLQ, increment retried count
    # On failure: keep in DLQ, increment still_failed count
    # return {:reply, {:ok, %{retried: N, still_failed: M}}, new_state}
    {:reply, {:ok, %{retried: 0, still_failed: 0}}, state}
  end

  @impl true
  def handle_call(:metrics, _from, state) do
    {:reply, state.metrics, state}
  end

  @impl true
  def handle_cast({:publish, topic, event}, state) do
    # TODO: find all compiled patterns in state.subscriptions that match topic
    # For each matching {ref, handler_fn}: dispatch via Task.start
    # Wrap the handler call to catch exceptions:
    #   on success → nothing
    #   on failure → add to DLQ with {topic, event, error, timestamp, attempt: 1}
    # Update history for this topic (use add_to_history/4)
    # Update metrics: events_total + 1, topics[topic].published + 1
    # HINT: Task.start(fn -> safe_dispatch(handler_fn, event, topic, state.dlq) end)
    {:noreply, state}
  end

  @impl true
  def handle_cast({:unsubscribe, ref}, state) do
    # TODO: remove all {^ref, _} entries from state.subscriptions
    # If a pid has no remaining subscriptions, demonitor it
    {:noreply, state}
  end

  @impl true
  def handle_cast(:dlq_purge, state) do
    {:noreply, %{state | dlq: :queue.new()}}
  end

  @impl true
  def handle_info({:DOWN, _mon_ref, :process, pid, _reason}, state) do
    # TODO: find all subscriptions for this pid in state.monitors
    # Remove those subscriptions from state.subscriptions
    # Remove the pid from state.monitors
    {:noreply, state}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp add_to_history(history, topic, event_entry, max_history) do
    queue = Map.get(history, topic, :queue.new())
    queue = :queue.in(event_entry, queue)
    queue =
      if :queue.len(queue) > max_history do
        {_, q} = :queue.out(queue)
        q
      else
        queue
      end
    Map.put(history, topic, queue)
  end
end
```

### Step 2: `lib/api_gateway/event_bus/topic_matcher.ex`

```elixir
defmodule ApiGateway.EventBus.TopicMatcher do
  @moduledoc """
  Wildcard topic matching for the event bus.

  Pattern rules:
    "*"  — matches exactly one segment
    "#"  — matches zero or more segments (at any position)
    else — literal segment match

  Examples:
    "orders.*"   matches "orders.created", "orders.updated"
    "*.created"  matches "orders.created", "users.created"
    "#"          matches everything
    "orders.#"   matches "orders.created", "orders.items.added"

  Compile patterns once at subscribe time; match on every publish.
  """

  @type compiled :: list(:single | :multi | String.t())

  @spec compile(String.t()) :: compiled()
  def compile(pattern) do
    # TODO: split pattern on ".", map each segment:
    #   "*" → :single
    #   "#" → :multi
    #   s   → s (literal string)
    []
  end

  @spec matches?(compiled(), String.t()) :: boolean()
  def matches?(compiled_pattern, topic) do
    segments = String.split(topic, ".")
    do_match(compiled_pattern, segments)
  end

  defp do_match([], []),                     do: true
  defp do_match([:multi | _], _),            do: true
  defp do_match([:single | rp], [_ | rt]),   do: do_match(rp, rt)
  defp do_match([seg | rp], [seg | rt]),     do: do_match(rp, rt)
  defp do_match(_, _),                       do: false
end
```

---

## Exercise 2: Wildcard matching — no additional files needed

The `TopicMatcher` above already has the interface. Your task is to implement
`compile/1` with the token mapping and verify it with the given tests.

---

## Exercise 3: `lib/api_gateway/event_bus/dlq_worker.ex`

```elixir
defmodule ApiGateway.EventBus.DLQWorker do
  @moduledoc """
  Background worker that periodically retries DLQ entries.

  Runs dlq_retry_all/1 every @retry_interval_ms. Uses exponential backoff
  per entry: entries that have failed many times wait longer between retries.
  """
  use GenServer

  @retry_interval_ms 30_000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    server = Keyword.get(opts, :server, ApiGateway.EventBus.Server)
    Process.send_after(self(), :retry, @retry_interval_ms)
    {:ok, %{server: server}}
  end

  @impl true
  def handle_info(:retry, state) do
    # TODO: call EventBus.Server.dlq_retry_all(state.server)
    # Log the result: "DLQ retry: retried=N still_failed=M"
    # Schedule the next retry
    Process.send_after(self(), :retry, @retry_interval_ms)
    {:noreply, state}
  end
end
```

---

## Exercise 4: `lib/api_gateway/event_bus/metrics_sampler.ex` and `supervisor.ex`

```elixir
defmodule ApiGateway.EventBus.MetricsSampler do
  @moduledoc """
  Calculates events-per-second by sampling events_total every 1,000ms.
  Updates the rate in the Server's metrics via update_rate/2.
  """
  use GenServer

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    server = Keyword.get(opts, :server, ApiGateway.EventBus.Server)
    :timer.send_interval(1_000, :sample)
    {:ok, %{last_total: 0, server: server}}
  end

  @impl true
  def handle_info(:sample, %{last_total: last, server: server} = state) do
    # TODO: call Server.metrics(server) to get the current events_total
    # rate = current_total - last
    # Call Server to update the rate in its metrics
    # return {:noreply, %{state | last_total: current_total}}
    {:noreply, state}
  end
end
```

```elixir
defmodule ApiGateway.EventBus.Supervisor do
  @moduledoc "Supervisor for the event bus component tree."
  use Supervisor

  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    children = [
      ApiGateway.EventBus.Server,
      ApiGateway.EventBus.MetricsSampler,
      ApiGateway.EventBus.DLQWorker
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/api_gateway/event_bus/topic_matcher_test.exs
defmodule ApiGateway.EventBus.TopicMatcherTest do
  use ExUnit.Case, async: true

  alias ApiGateway.EventBus.TopicMatcher

  defp matches?(pattern, topic) do
    TopicMatcher.matches?(TopicMatcher.compile(pattern), topic)
  end

  describe "literal matching" do
    test "exact match" do
      assert matches?("orders.created", "orders.created")
    end

    test "no match for different topic" do
      refute matches?("orders.created", "orders.updated")
    end

    test "no match for different depth" do
      refute matches?("orders.created", "orders")
    end
  end

  describe "* wildcard (exactly one segment)" do
    test "matches any single segment" do
      assert matches?("orders.*", "orders.created")
      assert matches?("orders.*", "orders.deleted")
    end

    test "does NOT match zero segments" do
      refute matches?("orders.*", "orders")
    end

    test "does NOT match two segments" do
      refute matches?("orders.*", "orders.items.added")
    end

    test "* at start" do
      assert matches?("*.created", "orders.created")
      assert matches?("*.created", "users.created")
    end
  end

  describe "# wildcard (zero or more segments)" do
    test "# alone matches everything" do
      assert matches?("#", "orders.created")
      assert matches?("#", "a.b.c.d")
      assert matches?("#", "single")
    end

    test "prefix.# matches prefix and any continuation" do
      assert matches?("orders.#", "orders.created")
      assert matches?("orders.#", "orders.items.added")
    end
  end

  describe "compile/1" do
    test "returns list of tokens" do
      assert TopicMatcher.compile("a.*.#") == ["a", :single, :multi]
    end

    test "is deterministic" do
      assert TopicMatcher.compile("x.y.z") == TopicMatcher.compile("x.y.z")
    end
  end
end
```

```elixir
# test/api_gateway/event_bus/server_test.exs
defmodule ApiGateway.EventBus.ServerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.EventBus.Server

  setup do
    {:ok, pid} = Server.start_link([])
    {:ok, server: pid}
  end

  test "subscribe returns a reference", %{server: server} do
    ref = Server.subscribe("orders.created", fn _e -> :ok end, server)
    assert is_reference(ref)
  end

  test "published event reaches the subscriber", %{server: server} do
    parent = self()
    Server.subscribe("orders.created", fn event -> send(parent, {:received, event}) end, server)

    Server.publish("orders.created", %{id: 1}, server)

    assert_receive {:received, %{id: 1}}, 500
  end

  test "fanout: all subscribers for a topic receive the event", %{server: server} do
    parent = self()

    for i <- 1..3 do
      Server.subscribe("orders.created", fn event ->
        send(parent, {:sub, i, event})
      end, server)
    end

    Server.publish("orders.created", %{order: "X"}, server)

    for i <- 1..3 do
      assert_receive {:sub, ^i, %{order: "X"}}, 500
    end
  end

  test "wildcard subscriber receives matching topics", %{server: server} do
    parent = self()
    Server.subscribe("orders.*", fn event -> send(parent, {:orders, event}) end, server)

    Server.publish("orders.created", %{id: 1}, server)
    Server.publish("orders.updated", %{id: 2}, server)
    Server.publish("users.created",  %{id: 3}, server)  # Should NOT arrive

    assert_receive {:orders, %{id: 1}}, 500
    assert_receive {:orders, %{id: 2}}, 500
    refute_receive {:orders, %{id: 3}}, 100
  end

  test "failed handler is sent to DLQ and does not crash other handlers", %{server: server} do
    parent = self()

    Server.subscribe("payments.*", fn _event -> raise "intentional failure" end, server)
    Server.subscribe("payments.*", fn event -> send(parent, {:ok_handler, event}) end, server)

    Server.publish("payments.failed", %{charge_id: "ch_001"}, server)

    # Good handler should still receive it
    assert_receive {:ok_handler, %{charge_id: "ch_001"}}, 500

    # DLQ should have the failure
    Process.sleep(100)
    assert length(Server.dlq_list(server)) >= 1
  end

  test "unsubscribe removes the handler" do
    # Use a fresh server to avoid interference
    {:ok, server} = Server.start_link([])
    parent        = self()

    ref = Server.subscribe("test.topic", fn e -> send(parent, {:got, e}) end, server)
    Server.publish("test.topic", %{n: 1}, server)
    assert_receive {:got, %{n: 1}}, 500

    Server.unsubscribe(ref, server)
    Server.publish("test.topic", %{n: 2}, server)
    refute_receive {:got, %{n: 2}}, 200
  end

  test "subscriber process death removes subscription", %{server: server} do
    parent = self()

    subscriber = spawn(fn ->
      Server.subscribe("cleanup.test", fn e -> send(parent, {:from_sub, e}) end, server)
      receive do: (:stop -> :ok)
    end)

    Process.sleep(50)
    send(subscriber, :stop)
    Process.sleep(50)

    # After subscriber dies, publishing should not raise or deliver to dead process
    Server.publish("cleanup.test", %{x: 1}, server)
    refute_receive {:from_sub, _}, 200
  end
end
```

```elixir
# test/api_gateway/event_bus/dlq_test.exs
defmodule ApiGateway.EventBus.DLQTest do
  use ExUnit.Case, async: true

  alias ApiGateway.EventBus.Server

  setup do
    {:ok, pid} = Server.start_link([])
    {:ok, server: pid}
  end

  test "failed handler adds an entry to DLQ", %{server: server} do
    Server.subscribe("dlq.test", fn _e -> raise "boom" end, server)
    Server.publish("dlq.test", %{id: 1}, server)

    Process.sleep(100)

    dlq = Server.dlq_list(server)
    assert length(dlq) >= 1
  end

  test "successful dlq_retry_all removes entries", %{server: server} do
    parent = self()

    # Subscribe handler that fails on first attempt, succeeds on retry
    # Use an Agent to track attempts
    {:ok, counter} = Agent.start_link(fn -> 0 end)

    Server.subscribe("dlq.retry", fn event ->
      n = Agent.get_and_update(counter, fn n -> {n, n + 1} end)
      if n == 0 do
        raise "first attempt fails"
      else
        send(parent, {:retried, event})
      end
    end, server)

    Server.publish("dlq.retry", %{id: 99}, server)
    Process.sleep(100)

    assert length(Server.dlq_list(server)) >= 1

    Server.dlq_retry_all(server)
    assert_receive {:retried, %{id: 99}}, 500
  end

  test "dlq_purge empties the dead-letter queue", %{server: server} do
    Server.subscribe("dlq.purge", fn _e -> raise "fail" end, server)
    Server.publish("dlq.purge", %{x: 1}, server)
    Process.sleep(100)

    assert length(Server.dlq_list(server)) >= 1
    Server.dlq_purge(server)

    assert Server.dlq_list(server) == []
  end
end
```

### Step 6: Run the tests

```bash
mix test test/api_gateway/event_bus/ --trace
```

---

## Trade-off analysis

| Aspect | `Task.start` dispatch | `GenStage` pipeline |
|--------|----------------------|---------------------|
| Back-pressure | None — publishes at any rate | Yes — consumers control demand |
| Complexity | Low | High |
| Best for | Event bus with independent consumers | Data pipelines with flow control |

| Aspect | In-process history (`:queue`) | External store (DETS, ETS) |
|--------|------------------------------|--------------------------|
| Survives restart | No | Yes (DETS) or No (ETS) |
| Memory usage | Bounded by `max_history` | Bounded by config |
| Query performance | O(N) per topic | O(1) for ETS |

| DLQ strategy | Trade-off |
|-------------|-----------|
| In-process queue | Lost on restart — simple |
| DETS persistence | Survives restart — adds I/O |
| External queue (SQS) | Production-grade — adds infrastructure |

---

## Common production mistakes

**1. Dispatching synchronously inside `handle_cast`**
Calling `handler_fn.(event)` directly inside `handle_cast` blocks the GenServer for
the duration of the handler. Use `Task.start/1` to dispatch asynchronously.

**2. Not monitoring subscriber processes**
When a subscriber process dies, its handler entry remains in `subscriptions`. Every
future publish attempt to that entry fails (sending to a dead process). Always call
`Process.monitor/1` on the caller PID and clean up on `:DOWN`.

**3. DLQ growing without a cap**
If handlers fail persistently, the DLQ grows unbounded. Set a maximum DLQ size and
discard the oldest entries when it is full, logging the discards.

**4. Not isolating test servers**
The event bus uses a named process (`__MODULE__`). In `async: true` tests, multiple
tests start the same named server and conflict. Always pass `name:` explicitly:
`Server.start_link([])` (anonymous PID, not registered name) and thread the PID
through test assertions.

**5. Handler exceptions propagating outside the Task**
If `Task.start` is used without wrapping the handler call in `try/rescue`, an exception
in the handler terminates the Task but also sends an exit signal to the GenServer if
`Task.start/1` is replaced with `Task.async/1` and the result is awaited. Use
`Task.start/1` (fire-and-forget) and wrap the handler call explicitly.

---

## Resources

- [GenServer — HexDocs](https://hexdocs.pm/elixir/GenServer.html)
- [Task — HexDocs](https://hexdocs.pm/elixir/Task.html)
- [`:queue` — Erlang Docs](https://www.erlang.org/doc/man/queue.html)
- [Phoenix.PubSub — HexDocs](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html) (production reference)
- [Broadway — HexDocs](https://hexdocs.pm/broadway/Broadway.html) (for flow-controlled pipelines)

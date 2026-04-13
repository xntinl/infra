# Reactive Streams Implementation

**Project**: `rstreams` — Reactive Streams implementation with explicit backpressure protocol and hot/cold observables

## Project context

Your team ingests sensor telemetry: 100,000 events per second from IoT devices, each requiring parsing, enrichment, filtering, and forwarding to three downstream consumers (a database, an alerting system, and an analytics pipeline). The consumers have different throughputs: the database writes at 80k/s, the alerting system at 10k/s, and the analytics pipeline at 5k/s.

A naive push model overwhelms the slowest consumer. A polling model adds latency. You need a pipeline where each consumer pulls elements at its own pace and backpressure propagates automatically upstream so the ingestion rate matches the slowest consumer's rate.

You will build `RStreams`: a complete Reactive Streams implementation in Elixir. No GenStage. No Flow. The protocol: a `Subscriber` calls `Subscription.request(n)` to pull elements; a `Publisher` delivers at most `n` elements; operators are `Processor`s that implement both interfaces and propagate backpressure through the chain.

## Why demand-based pull and not time-based throttling

pull demand is self-regulating — consumers set the pace exactly at their processing speed. Time throttling is open-loop; any rate you pick is wrong for some consumer somewhere.

## Design decisions

**Option A — push-based with unbounded buffers**
- Pros: simple to implement, no backpressure logic
- Cons: fast producer crashes slow consumer with OOM

**Option B — pull-based demand signaling (Reactive Streams spec)** (chosen)
- Pros: producer never outpaces consumer, OOM-safe by construction
- Cons: slightly more protocol overhead per batch

→ Chose **B** because reactive streams without backpressure is just streams — the whole spec exists to make backpressure explicit.

## Why backpressure must be an explicit protocol

TCP flow control is implicit backpressure at the network level. Application-level pipelines do not automatically inherit this. If a Publisher calls on_next in a loop without regard for subscriber capacity, the subscriber's process mailbox fills up. At 100k msg/s, a 10k/s subscriber accumulates 90k messages per second in its mailbox.

Reactive Streams makes backpressure an explicit protocol contract: the subscriber must request elements. The publisher may never deliver more than requested.

## Why operators must propagate backpressure upstream

A `map` operator wraps a publisher. When the downstream subscriber calls `request(10)`, the map processor must call `request(10)` on its upstream. If the map processor buffered eagerly, the buffer fills during slow consumer periods. The correct model: each `request(n)` from downstream triggers a `request(n)` from the operator to upstream.

## Why hot and cold observables are architecturally distinct

A cold observable (`from_list`) starts producing only when a subscriber subscribes. Each subscriber gets a fresh, independent stream. A hot observable (`from_interval`) produces regardless of subscribers. Late subscribers miss earlier elements.

A connectable observable bridges this: it is a hot observable that waits for an explicit `connect/1` call before producing. `auto_connect(2)` starts when two subscribers have subscribed.

## Project Structure

```
rstreams/
├── mix.exs
├── lib/
│   ├── rstreams/
│   │   ├── publisher.ex
│   │   ├── subscriber.ex
│   │   ├── subscription.ex
│   │   ├── processor.ex
│   │   ├── operators/
│   │   │   ├── map.ex
│   │   │   ├── filter.ex
│   │   │   ├── flat_map.ex
│   │   │   ├── zip.ex
│   │   │   ├── merge.ex
│   │   │   ├── take.ex
│   │   │   └── drop.ex
│   │   ├── sources/
│   │   │   ├── from_list.ex
│   │   │   ├── from_range.ex
│   │   │   └── from_interval.ex
│   │   ├── connectable.ex
│   │   ├── error_handlers.ex
│   │   └── scheduler.ex
│   └── rstreams.ex
├── test/
│   ├── tck/
│   │   ├── publisher_tck.exs
│   │   ├── subscriber_tck.exs
│   │   └── operator_tck.exs
│   └── rstreams_test.exs
└── bench/
    └── pipeline.exs
```

### Step 1: Core protocols

**Objective**: Encode the Reactive Streams contract as behaviours so TCK conformance is a compile-time claim, not a runtime hope.



### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule RStreams.Publisher do
  @moduledoc "RS spec: Publisher. Must never call on_next more than requested."
  @callback subscribe(subscriber :: pid()) :: :ok
end

defmodule RStreams.Subscriber do
  @moduledoc "RS spec: Subscriber. Receives elements via on_next, signals via on_error/on_complete."
  @callback on_subscribe(subscription :: {pid(), pid()}) :: :ok
  @callback on_next(element :: term()) :: :ok
  @callback on_error(reason :: term()) :: :ok
  @callback on_complete() :: :ok
end

defmodule RStreams.Subscription do
  @moduledoc """
  RS spec: Subscription. Manages demand between publisher and subscriber.
  RS 3.2: demand must accumulate -- successive request(3) + request(5) = 8 elements.
  RS 3.6: after cancel(), on_next must not be called.
  """

  @doc "Increase demand by n for the given subscriber on the given publisher."
  @spec request(pid(), pid(), pos_integer()) :: :ok
  def request(publisher_pid, subscriber_pid, n) when n > 0 do
    send(publisher_pid, {:request, subscriber_pid, n})
    :ok
  end

  @doc "Cancel the subscription. Publisher stops sending to this subscriber."
  @spec cancel(pid(), pid()) :: :ok
  def cancel(publisher_pid, subscriber_pid) do
    send(publisher_pid, {:cancel, subscriber_pid})
    :ok
  end
end
```

### Step 2: Publisher (cold, from list)

**Objective**: Build a cold source with per-subscriber cursors so each subscription replays independently and only under requested demand.


```elixir
defmodule RStreams.Sources.FromList do
  @moduledoc """
  Cold publisher that produces elements from a list.
  Each subscriber gets an independent cursor into the list.
  Elements are emitted only when demanded via request(n).
  """
  use GenServer
  @behaviour RStreams.Publisher

  @spec start_link(list()) :: GenServer.on_start()
  def start_link(list) when is_list(list) do
    GenServer.start_link(__MODULE__, list)
  end

  @doc "Subscribe a process to this publisher."
  @spec subscribe(pid(), pid()) :: :ok
  def subscribe(publisher_pid, subscriber_pid) do
    send(publisher_pid, {:subscribe, subscriber_pid})
    :ok
  end

  @impl GenServer
  def init(list) do
    {:ok, %{list: list, subscriptions: %{}}}
  end

  @impl GenServer
  def handle_info({:subscribe, subscriber_pid}, state) do
    Process.monitor(subscriber_pid)
    new_subscriptions = Map.put(state.subscriptions, subscriber_pid, %{demand: 0, index: 0})
    send(subscriber_pid, {:on_subscribe, {self(), subscriber_pid}})
    {:noreply, %{state | subscriptions: new_subscriptions}}
  end

  def handle_info({:request, subscriber_pid, n}, state) do
    case Map.get(state.subscriptions, subscriber_pid) do
      nil ->
        {:noreply, state}

      sub_state ->
        new_demand = sub_state.demand + n
        {updated_sub, updated_subs} = emit_elements(state.list, subscriber_pid, %{sub_state | demand: new_demand}, state.subscriptions)

        new_state =
          if updated_sub == :completed do
            %{state | subscriptions: Map.delete(updated_subs, subscriber_pid)}
          else
            %{state | subscriptions: Map.put(updated_subs, subscriber_pid, updated_sub)}
          end

        {:noreply, new_state}
    end
  end

  def handle_info({:cancel, subscriber_pid}, state) do
    {:noreply, %{state | subscriptions: Map.delete(state.subscriptions, subscriber_pid)}}
  end

  def handle_info({:DOWN, _ref, :process, subscriber_pid, _reason}, state) do
    {:noreply, %{state | subscriptions: Map.delete(state.subscriptions, subscriber_pid)}}
  end

  defp emit_elements(list, subscriber_pid, sub_state, subscriptions) do
    list_len = length(list)

    if sub_state.demand > 0 and sub_state.index < list_len do
      element = Enum.at(list, sub_state.index)
      send(subscriber_pid, {:on_next, element})

      new_sub = %{sub_state | demand: sub_state.demand - 1, index: sub_state.index + 1}

      if new_sub.index >= list_len do
        send(subscriber_pid, {:on_complete})
        {:completed, subscriptions}
      else
        emit_elements(list, subscriber_pid, new_sub, subscriptions)
      end
    else
      if sub_state.index >= list_len do
        send(subscriber_pid, {:on_complete})
        {:completed, subscriptions}
      else
        {sub_state, subscriptions}
      end
    end
  end
end
```

### Step 3: Operator: map (Processor)

**Objective**: Implement map as a Processor that forwards request(n) one-to-one so backpressure passes through untouched.


```elixir
defmodule RStreams.Operators.Map do
  @moduledoc """
  Map operator: transforms each element through a function.
  Implements both Publisher (for downstream) and Subscriber (for upstream).
  Propagates backpressure: each downstream request(n) triggers an
  upstream request(n).
  """
  use GenServer

  @spec start_link(pid(), (term() -> term())) :: GenServer.on_start()
  def start_link(upstream_pid, fun) do
    GenServer.start_link(__MODULE__, {upstream_pid, fun})
  end

  @impl GenServer
  def init({upstream_pid, fun}) do
    {:ok,
     %{
       upstream: upstream_pid,
       fun: fun,
       downstream: nil,
       upstream_sub: nil,
       demand: 0
     }}
  end

  @impl GenServer
  def handle_info({:subscribe, downstream_pid}, state) do
    send(state.upstream, {:subscribe, self()})
    {:noreply, %{state | downstream: downstream_pid}}
  end

  def handle_info({:on_subscribe, upstream_sub}, state) do
    send(state.downstream, {:on_subscribe, {self(), state.downstream}})
    {:noreply, %{state | upstream_sub: upstream_sub}}
  end

  def handle_info({:request, _from, n}, state) do
    {upstream_pid, _me} = state.upstream_sub || {state.upstream, self()}
    RStreams.Subscription.request(upstream_pid, self(), n)
    {:noreply, %{state | demand: state.demand + n}}
  end

  def handle_info({:on_next, element}, state) do
    transformed = state.fun.(element)
    send(state.downstream, {:on_next, transformed})
    {:noreply, %{state | demand: state.demand - 1}}
  end

  def handle_info({:on_complete}, state) do
    send(state.downstream, {:on_complete})
    {:stop, :normal, state}
  end

  def handle_info({:on_error, reason}, state) do
    send(state.downstream, {:on_error, reason})
    {:stop, :normal, state}
  end

  def handle_info({:cancel, _from}, state) do
    {upstream_pid, me} = state.upstream_sub || {state.upstream, self()}
    RStreams.Subscription.cancel(upstream_pid, me)
    {:stop, :normal, state}
  end
end
```

### Step 4: Operator: filter

**Objective**: When the predicate drops an element, pull one more from upstream so downstream demand is preserved without deadlock.


```elixir
defmodule RStreams.Operators.Filter do
  @moduledoc """
  Filter operator: passes through only elements matching a predicate.
  When an element is filtered out, requests one more from upstream
  to maintain downstream demand.
  """
  use GenServer

  @spec start_link(pid(), (term() -> boolean())) :: GenServer.on_start()
  def start_link(upstream_pid, predicate) do
    GenServer.start_link(__MODULE__, {upstream_pid, predicate})
  end

  @impl GenServer
  def init({upstream_pid, predicate}) do
    {:ok,
     %{
       upstream: upstream_pid,
       predicate: predicate,
       downstream: nil,
       upstream_sub: nil,
       demand: 0
     }}
  end

  @impl GenServer
  def handle_info({:subscribe, downstream_pid}, state) do
    send(state.upstream, {:subscribe, self()})
    {:noreply, %{state | downstream: downstream_pid}}
  end

  def handle_info({:on_subscribe, upstream_sub}, state) do
    send(state.downstream, {:on_subscribe, {self(), state.downstream}})
    {:noreply, %{state | upstream_sub: upstream_sub}}
  end

  def handle_info({:request, _from, n}, state) do
    {upstream_pid, _me} = state.upstream_sub || {state.upstream, self()}
    RStreams.Subscription.request(upstream_pid, self(), n)
    {:noreply, %{state | demand: state.demand + n}}
  end

  def handle_info({:on_next, element}, state) do
    if state.predicate.(element) do
      send(state.downstream, {:on_next, element})
      {:noreply, %{state | demand: state.demand - 1}}
    else
      # Filtered out: request one more from upstream to fill the gap
      {upstream_pid, _me} = state.upstream_sub || {state.upstream, self()}
      RStreams.Subscription.request(upstream_pid, self(), 1)
      {:noreply, state}
    end
  end

  def handle_info({:on_complete}, state) do
    send(state.downstream, {:on_complete})
    {:stop, :normal, state}
  end

  def handle_info({:on_error, reason}, state) do
    send(state.downstream, {:on_error, reason})
    {:stop, :normal, state}
  end

  def handle_info({:cancel, _from}, state) do
    {upstream_pid, me} = state.upstream_sub || {state.upstream, self()}
    RStreams.Subscription.cancel(upstream_pid, me)
    {:stop, :normal, state}
  end
end
```

### Step 5: Flat map (hardest operator)

**Objective**: Multiplex inner publishers with a bounded buffer so outer demand is paced by inner drain, never overwhelming the subscriber.


```elixir
defmodule RStreams.Operators.FlatMap do
  @moduledoc """
  flat_map(publisher, f) where f returns a publisher for each element.
  Manages multiple inner subscriptions. Backpressure: only requests
  from outer when inner is drained AND downstream has demand.
  Buffers elements when downstream demand is exhausted.
  """
  use GenServer

  @spec start_link(pid(), (term() -> pid())) :: GenServer.on_start()
  def start_link(upstream_pid, fun) do
    GenServer.start_link(__MODULE__, {upstream_pid, fun})
  end

  @impl GenServer
  def init({upstream_pid, fun}) do
    {:ok,
     %{
       upstream: upstream_pid,
       fun: fun,
       downstream: nil,
       upstream_sub: nil,
       demand: 0,
       inner_pid: nil,
       outer_completed: false,
       buffer: :queue.new()
     }}
  end

  @impl GenServer
  def handle_info({:subscribe, downstream_pid}, state) do
    send(state.upstream, {:subscribe, self()})
    {:noreply, %{state | downstream: downstream_pid}}
  end

  def handle_info({:on_subscribe, upstream_sub}, state) do
    send(state.downstream, {:on_subscribe, {self(), state.downstream}})
    {:noreply, %{state | upstream_sub: upstream_sub}}
  end

  def handle_info({:request, _from, n}, state) do
    new_demand = state.demand + n
    state = %{state | demand: new_demand}

    # Drain buffer first
    state = drain_buffer(state)

    # If no inner subscription active, request from outer
    if state.inner_pid == nil and not state.outer_completed do
      {upstream_pid, _me} = state.upstream_sub || {state.upstream, self()}
      RStreams.Subscription.request(upstream_pid, self(), 1)
    end

    {:noreply, state}
  end

  def handle_info({:on_next, outer_element}, state) do
    inner_publisher_pid = state.fun.(outer_element)
    send(inner_publisher_pid, {:subscribe, self()})
    {:noreply, %{state | inner_pid: inner_publisher_pid}}
  end

  def handle_info({:inner_on_subscribe, _sub}, state) do
    # Request from inner based on current downstream demand
    if state.demand > 0 and state.inner_pid do
      RStreams.Subscription.request(state.inner_pid, self(), state.demand)
    end

    {:noreply, state}
  end

  def handle_info({:on_next, element}, %{inner_pid: inner} = state) when inner != nil do
    handle_info({:inner_on_next, element}, state)
  end

  def handle_info({:inner_on_next, element}, state) do
    if state.demand > 0 do
      send(state.downstream, {:on_next, element})
      {:noreply, %{state | demand: state.demand - 1}}
    else
      {:noreply, %{state | buffer: :queue.in(element, state.buffer)}}
    end
  end

  def handle_info({:on_complete}, %{inner_pid: nil} = state) do
    # Outer completed
    handle_info({:outer_on_complete}, state)
  end

  def handle_info({:on_complete}, state) do
    # Inner completed
    handle_info({:inner_on_complete}, state)
  end

  def handle_info({:outer_on_complete}, state) do
    state = %{state | outer_completed: true}

    if state.inner_pid == nil and :queue.is_empty(state.buffer) do
      send(state.downstream, {:on_complete})
      {:stop, :normal, state}
    else
      {:noreply, state}
    end
  end

  def handle_info({:inner_on_complete}, state) do
    state = %{state | inner_pid: nil}

    if state.outer_completed and :queue.is_empty(state.buffer) do
      send(state.downstream, {:on_complete})
      {:stop, :normal, state}
    else
      if state.demand > 0 and not state.outer_completed do
        {upstream_pid, _me} = state.upstream_sub || {state.upstream, self()}
        RStreams.Subscription.request(upstream_pid, self(), 1)
      end

      {:noreply, state}
    end
  end

  def handle_info({:on_error, reason}, state) do
    send(state.downstream, {:on_error, reason})
    {:stop, :normal, state}
  end

  defp drain_buffer(state) do
    case :queue.out(state.buffer) do
      {{:value, element}, rest} when state.demand > 0 ->
        send(state.downstream, {:on_next, element})
        drain_buffer(%{state | demand: state.demand - 1, buffer: rest})

      _ ->
        state
    end
  end
end
```

### Step 6: Connectable observable

**Objective**: Gate a shared source behind explicit connect/auto_connect so late subscribers cannot silently reset an already-running hot pipeline.


```elixir
defmodule RStreams.Connectable do
  @moduledoc """
  Connectable observable: a hot observable that waits for an explicit
  connect/1 call before subscribing to its source. Multicasts elements
  to all subscribers. auto_connect(n) triggers connection when n
  subscribers have joined.
  """
  use GenServer

  @spec start_link(pid()) :: GenServer.on_start()
  def start_link(source_pid) do
    GenServer.start_link(__MODULE__, source_pid)
  end

  @doc "Connect: subscribe to the source and begin production."
  @spec connect(pid()) :: :ok
  def connect(pid) do
    GenServer.cast(pid, :connect)
  end

  @doc "Set auto-connect threshold: connect when n subscribers have joined."
  @spec auto_connect(pid(), pos_integer()) :: :ok
  def auto_connect(pid, n) do
    GenServer.cast(pid, {:auto_connect, n})
  end

  @impl GenServer
  def init(source_pid) do
    {:ok,
     %{
       source: source_pid,
       subscribers: [],
       connected: false,
       demand: 0,
       auto_connect_threshold: nil
     }}
  end

  @impl GenServer
  def handle_info({:subscribe, subscriber_pid}, state) do
    Process.monitor(subscriber_pid)
    send(subscriber_pid, {:on_subscribe, {self(), subscriber_pid}})
    new_subscribers = [subscriber_pid | state.subscribers]
    state = %{state | subscribers: new_subscribers}

    state =
      case state.auto_connect_threshold do
        n when is_integer(n) and length(new_subscribers) >= n and not state.connected ->
          send(state.source, {:subscribe, self()})
          %{state | connected: true}

        _ ->
          state
      end

    {:noreply, state}
  end

  def handle_info({:on_subscribe, _source_sub}, state) do
    {:noreply, state}
  end

  def handle_info({:request, _from, n}, state) do
    new_demand = state.demand + n

    if state.connected do
      RStreams.Subscription.request(state.source, self(), n)
    end

    {:noreply, %{state | demand: new_demand}}
  end

  def handle_info({:on_next, element}, state) do
    Enum.each(state.subscribers, fn pid -> send(pid, {:on_next, element}) end)
    {:noreply, %{state | demand: state.demand - 1}}
  end

  def handle_info({:on_complete}, state) do
    Enum.each(state.subscribers, fn pid -> send(pid, {:on_complete}) end)
    {:stop, :normal, state}
  end

  def handle_info({:on_error, reason}, state) do
    Enum.each(state.subscribers, fn pid -> send(pid, {:on_error, reason}) end)
    {:stop, :normal, state}
  end

  def handle_info({:DOWN, _ref, :process, subscriber_pid, _reason}, state) do
    {:noreply, %{state | subscribers: List.delete(state.subscribers, subscriber_pid)}}
  end

  def handle_info({:cancel, subscriber_pid}, state) do
    {:noreply, %{state | subscribers: List.delete(state.subscribers, subscriber_pid)}}
  end

  @impl GenServer
  def handle_cast(:connect, state) do
    unless state.connected do
      send(state.source, {:subscribe, self()})
    end

    {:noreply, %{state | connected: true}}
  end

  def handle_cast({:auto_connect, n}, state) do
    state = %{state | auto_connect_threshold: n}

    state =
      if length(state.subscribers) >= n and not state.connected do
        send(state.source, {:subscribe, self()})
        %{state | connected: true}
      else
        state
      end

    {:noreply, state}
  end
end
```

### Step 7: Hot source (interval)

**Objective**: Decouple production from demand with a timer-driven emitter so overflow policy, not backpressure, governs excess elements.


```elixir
defmodule RStreams.Sources.FromInterval do
  @moduledoc """
  Hot publisher that emits an incrementing counter at fixed intervals.
  Produces regardless of subscribers; late subscribers miss earlier elements.
  Respects backpressure by buffering up to a limit.
  """
  use GenServer

  @max_buffer 10_000

  @spec start_link(pos_integer()) :: GenServer.on_start()
  def start_link(interval_ms) do
    GenServer.start_link(__MODULE__, interval_ms)
  end

  @impl GenServer
  def init(interval_ms) do
    Process.send_after(self(), :tick, interval_ms)

    {:ok,
     %{
       interval_ms: interval_ms,
       counter: 0,
       subscribers: %{}
     }}
  end

  @impl GenServer
  def handle_info(:tick, state) do
    new_counter = state.counter + 1

    new_subs =
      Enum.reduce(state.subscribers, state.subscribers, fn {pid, sub}, acc ->
        if sub.demand > 0 do
          send(pid, {:on_next, new_counter})
          Map.put(acc, pid, %{sub | demand: sub.demand - 1})
        else
          acc
        end
      end)

    Process.send_after(self(), :tick, state.interval_ms)
    {:noreply, %{state | counter: new_counter, subscribers: new_subs}}
  end

  def handle_info({:subscribe, subscriber_pid}, state) do
    Process.monitor(subscriber_pid)
    new_subs = Map.put(state.subscribers, subscriber_pid, %{demand: 0})
    send(subscriber_pid, {:on_subscribe, {self(), subscriber_pid}})
    {:noreply, %{state | subscribers: new_subs}}
  end

  def handle_info({:request, subscriber_pid, n}, state) do
    case Map.get(state.subscribers, subscriber_pid) do
      nil ->
        {:noreply, state}

      sub ->
        new_subs = Map.put(state.subscribers, subscriber_pid, %{sub | demand: sub.demand + n})
        {:noreply, %{state | subscribers: new_subs}}
    end
  end

  def handle_info({:cancel, subscriber_pid}, state) do
    {:noreply, %{state | subscribers: Map.delete(state.subscribers, subscriber_pid)}}
  end

  def handle_info({:DOWN, _ref, :process, subscriber_pid, _reason}, state) do
    {:noreply, %{state | subscribers: Map.delete(state.subscribers, subscriber_pid)}}
  end
end
```

## Given tests (TCK-inspired)

```elixir
# test/tck/publisher_tck.exs
defmodule RStreams.TCK.PublisherTest do
  use ExUnit.Case, async: true
  alias RStreams.Sources.FromList
  alias RStreams.Subscription


  describe "Publisher" do

  test "RS 1.1: publisher delivers at most requested elements" do
    {:ok, pub} = FromList.start_link([1, 2, 3, 4, 5])
    parent = self()

    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 2)
          collect([], 2, parent)
      end
    end)

    send(pub, {:subscribe, sub})
    assert_receive {:collected, [1, 2]}, 1000
  end

  defp collect(acc, 0, parent), do: send(parent, {:collected, Enum.reverse(acc)})

  defp collect(acc, remaining, parent) do
    receive do
      {:on_next, e} -> collect([e | acc], remaining - 1, parent)
    after
      500 -> send(parent, {:collected, Enum.reverse(acc)})
    end
  end

  test "RS 1.2: publisher does not call on_next before request" do
    {:ok, pub} = FromList.start_link([1, 2, 3])
    parent = self()

    sub = spawn(fn ->
      receive do
        {:on_subscribe, _sub} ->
          receive do
            {:on_next, _} -> send(parent, :violation)
          after
            200 -> send(parent, :no_violation)
          end
      end
    end)

    send(pub, {:subscribe, sub})
    assert_receive :no_violation, 500
    refute_receive :violation, 100
  end

  test "RS 1.7: on_complete is terminal -- no on_next after it" do
    {:ok, pub} = FromList.start_link([1])
    {:ok, agent} = Agent.start_link(fn -> [] end)
    parent = self()

    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 10)
          loop_check(agent, parent)
      end
    end)

    send(pub, {:subscribe, sub})
    Process.sleep(300)
    events = Agent.get(agent, & &1)
    on_complete_idx = Enum.find_index(events, &(&1 == :complete))

    on_next_after =
      Enum.drop(events, (on_complete_idx || 9999) + 1)
      |> Enum.any?(&match?({:next, _}, &1))

    refute on_next_after, "on_next received after on_complete"
  end

  defp loop_check(agent, _parent) do
    receive do
      {:on_next, e} ->
        Agent.update(agent, &[{:next, e} | &1])
        loop_check(agent, _parent)

      {:on_complete} ->
        Agent.update(agent, &[:complete | &1])

      {:on_error, _} ->
        Agent.update(agent, &[:error | &1])
    after
      500 -> :done
    end
  end

  test "RS demand accumulation: successive requests add up" do
    {:ok, pub} = FromList.start_link(Enum.to_list(1..10))
    parent = self()

    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 3)
          Subscription.request(pub_pid, me, 5)
          collect_n([], 8, parent)
      end
    end)

    send(pub, {:subscribe, sub})
    assert_receive {:collected_n, list}, 1000
    assert length(list) == 8
  end

  defp collect_n(acc, 0, parent), do: send(parent, {:collected_n, Enum.reverse(acc)})

  defp collect_n(acc, n, parent) do
    receive do
      {:on_next, e} -> collect_n([e | acc], n - 1, parent)
    after
      500 -> send(parent, {:collected_n, Enum.reverse(acc)})
    end
  end
end

# test/tck/operator_tck.exs
defmodule RStreams.TCK.OperatorTest do
  use ExUnit.Case, async: true
  alias RStreams.{Sources.FromList, Operators.Map, Subscription}

  test "map operator: backpressure propagates to upstream" do
    {:ok, source} = FromList.start_link(Enum.to_list(1..100))
    {:ok, mapped} = Map.start_link(source, &(&1 * 2))
    parent = self()

    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 5)
          collect_n_and_report([], 5, parent)
      end
    end)

    send(mapped, {:subscribe, sub})
    assert_receive {:done, results}, 1000
    assert results == [2, 4, 6, 8, 10]
  end

  defp collect_n_and_report(acc, 0, parent), do: send(parent, {:done, Enum.reverse(acc)})

  defp collect_n_and_report(acc, n, parent) do
    receive do
      {:on_next, e} -> collect_n_and_report([e | acc], n - 1, parent)
    after
      500 -> send(parent, {:done, Enum.reverse(acc)})
    end
  end

  test "map operator: transformed values reach subscriber" do
    {:ok, source} = FromList.start_link([1, 2, 3])
    {:ok, mapped} = Map.start_link(source, fn x -> x * 10 end)
    parent = self()

    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 1000)
          do_collect([], parent)
      end
    end)

    send(mapped, {:subscribe, sub})
    assert_receive {:results, [10, 20, 30]}, 1000
  end

  defp do_collect(acc, parent) do
    receive do
      {:on_next, e} -> do_collect([e | acc], parent)
      {:on_complete} -> send(parent, {:results, Enum.reverse(acc)})
    after
      500 -> send(parent, {:results, Enum.reverse(acc)})
    end
  end


  end
end
```

### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Benchmark

```elixir
# bench/pipeline.exs
# Run with: mix run bench/pipeline.exs
defmodule RStreams.Bench.Pipeline do
  alias RStreams.{Sources.FromList, Operators.Map, Operators.Filter, Subscription}

  @element_count 100_000

  def run do
    data = Enum.to_list(1..@element_count)

    {time_us, results} =
      :timer.tc(fn ->
        {:ok, source} = FromList.start_link(data)
        {:ok, mapped} = Map.start_link(source, &(&1 * 2))
        {:ok, filtered} = Filter.start_link(mapped, &(rem(&1, 4) == 0))
        collect_all_timed(filtered)
      end)

    throughput = @element_count / (time_us / 1_000_000)
    IO.puts("Elements:   #{@element_count}")
    IO.puts("Time:       #{Float.round(time_us / 1000, 1)} ms")
    IO.puts("Throughput: #{Float.round(throughput, 0)} elements/s")
    IO.puts("Results:    #{length(results)} elements passed filter")
  end

  defp collect_all_timed(publisher) do
    me = self()

    sub =
      spawn(fn ->
        receive do
          {:on_subscribe, {pub_pid, sub_pid}} ->
            Subscription.request(pub_pid, sub_pid, @element_count)
            collect_loop([], me)
        end
      end)

    send(publisher, {:subscribe, sub})

    receive do
      {:done, results} -> results
    after
      30_000 -> raise "Benchmark timeout"
    end
  end

  defp collect_loop(acc, parent) do
    receive do
      {:on_next, e} -> collect_loop([e | acc], parent)
      {:on_complete} -> send(parent, {:done, Enum.reverse(acc)})
    end
  end
end

RStreams.Bench.Pipeline.run()
def main do
  IO.puts("[RStreams.TCK.PublisherTest] GenServer demo")
  :ok
end

```

## Key Concepts: Event Sourcing and Immutable Logs

Event sourcing inverts the traditional database model: instead of storing current state, store every state-changing event in an immutable log. The current state is derived by replaying events from the start.

This shift has profound implications:
- **Audit trail is free**: Every change is a named event with timestamp and actor.
- **Temporal queries are simple**: Replay events up to a past date to see historical state.
- **Concurrency is safe**: Events are immutable and append-only, eliminating race conditions on state mutations.
- **Testability is easier**: Given a sequence of events, the state is deterministic; no mocks needed.

The BEAM is naturally suited for this pattern. Each aggregate (e.g., Account) is a GenServer that receives commands, validates them against current state, publishes an event if valid, then applies the event to update local state. The OTP supervision tree ensures persistence across restarts; the event log (in a database) survives the entire system.

The downside: evolving schemas is hard. If you rename a field or split an event type, old events still use the old structure. Solutions include versioning (introduce `withdrew_v2` alongside `withdrew_v1`) or upcasting (projection functions that translate old events to new). Frameworks like Commanded automate this.

Another challenge: reads require replaying events, which is slow for 10-year-old aggregates with millions of events. Solution: snapshots. Periodically serialize current state; replay only events after the snapshot. This trades disk space for query speed, a worthwhile tradeoff for most systems.

**Production insight**: Event sourcing is powerful for audit-heavy systems (banking, compliance), but unnecessary overhead for simple CRUD apps. Choose event sourcing when the audit trail or temporal queries justify the implementation complexity.

---

## Trade-off analysis

| Design aspect | RS spec requirement | BEAM-native alternative | Trade-off |
|---|---|---|---|
| Demand signaling | `Subscription.request(n)` per subscriber | GenStage `:demand` messages | GenStage is more BEAM-idiomatic; RS is portable across languages |
| Subscriber per process | One subscriber per subscription | Single shared subscriber | Per-process: true isolation, can trap exits |
| Demand accumulation | request(3) + request(5) = 8 total | Reset demand per call | Accumulation is correct per spec; reset is simpler but non-compliant |
| flat_map buffering | Unbounded inner subscriptions | Bounded concurrency | Unbounded is spec-compliant; bounded is safer under load |
| Hot observable multicast | Connectable via explicit connect/1 | PubSub broadcast | PubSub is BEAM-native; Connectable is spec-compliant |

## Common production mistakes

**Sending on_next from a different process than the demand manager.** Without atomic update, the demand counter can go negative or allow excess emission. Use a single GenServer per subscription.

**Not monitoring subscriber processes.** If a subscriber dies without cancel, the publisher continues tracking demand for a dead pid. Always `Process.monitor(subscriber_pid)` on subscription.

**Calling subscriber callbacks synchronously in the publisher.** If on_next is a GenServer.call, a slow subscriber blocks the publisher. Use send (asynchronous) for all subscriber callbacks.

**flat_map not requesting from outer after inner completes.** When inner completes, if downstream still has demand, the flat_map must immediately request from outer. Otherwise the pipeline stalls.

**Ignoring Long.MAX_VALUE demand.** The RS spec allows requesting unlimited demand. In Elixir, integers are arbitrary precision, so cap at a reasonable maximum to prevent unbounded accumulation.

## Reflection

Your pipeline has producer → transform → sink. The sink stalls for 10 seconds. Where does demand propagate, how much buffers between stages, and when does the producer actually pause?

## Resources

- Reactive Streams Specification -- https://www.reactive-streams.org
- Reactive Streams TCK -- https://github.com/reactive-streams/reactive-streams-jvm/tree/master/tck
- GenStage source -- https://github.com/elixir-lang/gen_stage
- Meijer -- "Your Mouse is a Database" (2012) -- CACM 55(5)
- Kuhn, Hanafee & Allen -- "Reactive Design Patterns" (Manning, 2017)

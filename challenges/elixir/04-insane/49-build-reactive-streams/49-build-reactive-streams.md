# Reactive Streams Implementation

**Project**: `rstreams` — Reactive Streams implementation with explicit backpressure protocol and hot/cold observables

## Project context

Your team ingests sensor telemetry: 100,000 events per second from IoT devices, each requiring parsing, enrichment, filtering, and forwarding to three downstream consumers (a database, an alerting system, and an analytics pipeline). The consumers have different throughputs: the database writes at 80k/s, the alerting system at 10k/s, and the analytics pipeline at 5k/s.

A naive push model overwhelms the slowest consumer. A polling model adds latency. You need a pipeline where each consumer pulls elements at its own pace and that backpressure propagates automatically upstream so the ingestion rate matches the slowest consumer's rate.

You will build `RStreams`: a complete Reactive Streams implementation in Elixir. No GenStage. No Flow. The protocol: a `Subscriber` calls `Subscription.request(n)` to pull elements; a `Publisher` delivers at most `n` elements; operators are `Processor`s that implement both interfaces and propagate backpressure through the chain.

## Why backpressure must be an explicit protocol, not a side effect

TCP flow control is implicit backpressure at the network level. Application-level pipelines do not automatically inherit this. If a `Publisher` calls `on_next` in a loop without regard for the subscriber's capacity, the subscriber's process mailbox fills up. At 100k msg/s, a 10k/s subscriber accumulates 90k messages per second in its mailbox. After 10 minutes, that is 54 million pending messages consuming gigabytes of memory.

Reactive Streams makes backpressure an explicit protocol contract: the subscriber must request elements. The publisher may never deliver more than requested. This inverts the control direction: the slowest consumer drives the rate for the entire pipeline.

## Why operators must propagate backpressure upstream, not downstream

A `map` operator wraps a publisher. When the downstream subscriber calls `request(10)`, the `map` processor must call `request(10)` on its upstream. If the `map` processor buffered eagerly (fetched 1000 elements before any downstream request), the buffer fills during slow consumer periods. The correct model: each `request(n)` from downstream triggers a `request(n)` from the operator to upstream. The backpressure propagates transitively through the entire chain.

## Why hot and cold observables are architecturally distinct

A cold observable (`from_list`) starts producing only when a subscriber subscribes. Each subscriber gets a fresh, independent stream from the beginning. A hot observable (`from_interval`) produces regardless of subscribers. Late subscribers miss elements emitted before they subscribed.

A connectable observable bridges this: it is a hot observable that waits for an explicit `connect/1` call before producing. `auto_connect(2)` starts when two subscribers have subscribed. This enables fan-out: two slow consumers on a single fast producer, each seeing the same elements.

## Project Structure

```
rstreams/
├── mix.exs
├── lib/
│   ├── rstreams/
│   │   ├── publisher.ex           # Publisher behaviour: subscribe/1
│   │   ├── subscriber.ex          # Subscriber behaviour: on_subscribe/1, on_next/1, etc.
│   │   ├── subscription.ex        # Subscription: request/2, cancel/1
│   │   ├── processor.ex           # Processor behaviour (Publisher + Subscriber)
│   │   ├── operators/
│   │   │   ├── map.ex             # map(publisher, f)
│   │   │   ├── filter.ex          # filter(publisher, pred)
│   │   │   ├── flat_map.ex        # flat_map(publisher, f) — hardest operator
│   │   │   ├── zip.ex             # zip(pub_a, pub_b)
│   │   │   ├── merge.ex           # merge(list_of_publishers)
│   │   │   ├── take.ex            # take(publisher, n)
│   │   │   └── drop.ex            # drop(publisher, n)
│   │   ├── sources/
│   │   │   ├── from_list.ex       # Cold: from list
│   │   │   ├── from_range.ex      # Cold: from range
│   │   │   └── from_interval.ex   # Hot: timer-based
│   │   ├── connectable.ex         # Connectable observable: connect/1, auto_connect/2
│   │   ├── error_handlers.ex      # on_error_return, retry, retry_when
│   │   └── scheduler.ex           # Scheduler: immediate, new_process, pool
│   └── rstreams.ex
├── test/
│   ├── tck/
│   │   ├── publisher_tck.exs      # RS spec publisher rules
│   │   ├── subscriber_tck.exs     # RS spec subscriber rules
│   │   └── operator_tck.exs      # Backpressure propagation through operators
│   └── rstreams_test.exs
└── bench/
    └── pipeline.exs
```

### Step 1: Core protocols

```elixir
defmodule RStreams.Publisher do
  @moduledoc "RS spec: Publisher. Must never call on_next more than requested."
  @callback subscribe(subscriber :: pid()) :: :ok
end

defmodule RStreams.Subscriber do
  @callback on_subscribe(subscription :: {pid(), pid()}) :: :ok
  @callback on_next(element :: term()) :: :ok
  @callback on_error(reason :: term()) :: :ok
  @callback on_complete() :: :ok
end

defmodule RStreams.Subscription do
  @doc """
  Increase demand by n for the given subscriber on the given publisher.
  RS 3.2: demand must accumulate — successive request(3) + request(5) = 8 elements.
  RS 3.6: after cancel(), on_next must not be called.
  """
  def request(publisher_pid, subscriber_pid, n) when n > 0 do
    send(publisher_pid, {:request, subscriber_pid, n})
  end

  def cancel(publisher_pid, subscriber_pid) do
    send(publisher_pid, {:cancel, subscriber_pid})
  end
end
```

### Step 2: Publisher (cold, from list)

```elixir
defmodule RStreams.Sources.FromList do
  use GenServer
  @behaviour RStreams.Publisher

  def start_link(list) when is_list(list) do
    GenServer.start_link(__MODULE__, list)
  end

  def subscribe(publisher_pid, subscriber_pid) do
    send(publisher_pid, {:subscribe, subscriber_pid})
  end

  def init(list) do
    # State per subscriber: %{subscriber_pid => %{demand: 0, index: 0}}
    {:ok, %{list: list, subscriptions: %{}}}
  end

  def handle_info({:subscribe, subscriber_pid}, state) do
    Process.monitor(subscriber_pid)
    new_subscriptions = Map.put(state.subscriptions, subscriber_pid, %{demand: 0, index: 0})
    send(subscriber_pid, {:on_subscribe, {self(), subscriber_pid}})
    {:noreply, %{state | subscriptions: new_subscriptions}}
  end

  def handle_info({:request, subscriber_pid, n}, state) do
    case Map.get(state.subscriptions, subscriber_pid) do
      nil -> {:noreply, state}
      sub_state ->
        new_demand = sub_state.demand + n
        # TODO: emit min(new_demand, remaining_elements) on_next calls to subscriber_pid
        # TODO: decrement demand for each emitted element
        # TODO: if no more elements: send {:on_complete} to subscriber_pid, remove subscription
        # HINT: use a recursive helper that emits while demand > 0 and elements remain
        updated = %{sub_state | demand: new_demand}
        {:noreply, %{state | subscriptions: Map.put(state.subscriptions, subscriber_pid, updated)}}
    end
  end

  def handle_info({:cancel, subscriber_pid}, state) do
    # TODO: remove subscriber from subscriptions; stop emitting to them
    {:noreply, %{state | subscriptions: Map.delete(state.subscriptions, subscriber_pid)}}
  end

  def handle_info({:DOWN, _ref, :process, subscriber_pid, _reason}, state) do
    {:noreply, %{state | subscriptions: Map.delete(state.subscriptions, subscriber_pid)}}
  end
end
```

### Step 3: Operator: map (Processor)

```elixir
defmodule RStreams.Operators.Map do
  use GenServer

  def start_link(upstream_pid, fun) do
    GenServer.start_link(__MODULE__, {upstream_pid, fun})
  end

  def init({upstream_pid, fun}) do
    {:ok, %{
      upstream: upstream_pid,
      fun: fun,
      downstream: nil,         # set when a subscriber subscribes to us
      upstream_sub: nil,       # set when we subscribe to upstream
      demand: 0
    }}
  end

  # A downstream subscriber subscribes to the map operator
  def handle_info({:subscribe, downstream_pid}, state) do
    # TODO: subscribe ourselves to upstream (we become upstream's subscriber)
    send(state.upstream, {:subscribe, self()})
    {:noreply, %{state | downstream: downstream_pid}}
  end

  # Upstream sends us our subscription
  def handle_info({:on_subscribe, upstream_sub}, state) do
    send(state.downstream, {:on_subscribe, {self(), state.downstream}})
    {:noreply, %{state | upstream_sub: upstream_sub}}
  end

  # Downstream requests n elements from us
  def handle_info({:request, _from, n}, state) do
    # TODO: propagate demand n upstream
    # HINT: RStreams.Subscription.request(upstream_pid, self(), n)
    {:noreply, %{state | demand: state.demand + n}}
  end

  # Upstream delivers an element
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
    # TODO: cancel our subscription to upstream
    {:stop, :normal, state}
  end
end
```

### Step 4: Flat map (hardest operator)

```elixir
defmodule RStreams.Operators.FlatMap do
  use GenServer

  @moduledoc """
  flat_map(publisher, f) where f returns a publisher for each element.
  Manages multiple inner subscriptions concurrently.
  Backpressure: only request from outer when inner is drained AND downstream demanded.
  """

  def init({upstream_pid, fun}) do
    {:ok, %{
      upstream: upstream_pid,
      fun: fun,
      downstream: nil,
      demand: 0,
      inner_subscriptions: [],  # active inner publisher pids
      outer_completed: false,
      buffer: :queue.new()
    }}
  end

  def handle_info({:on_next, outer_element}, state) do
    # TODO: apply fun to outer_element → inner_publisher_pid
    # TODO: subscribe self to inner_publisher as inner_subscriber_{ref}
    # TODO: track inner subscription; request 1 from inner based on current demand
    {:noreply, state}
  end

  def handle_info({:inner_on_next, element}, state) do
    # TODO: if demand > 0: send to downstream, decrement demand, request 1 more from inner
    # TODO: if demand == 0: buffer element in state.buffer
    {:noreply, state}
  end

  def handle_info({:inner_on_complete, inner_pid}, state) do
    # TODO: remove inner_pid from inner_subscriptions
    # TODO: if outer_completed and inner_subscriptions empty: send on_complete to downstream
    # TODO: if demand > 0: request 1 more from outer upstream
    {:noreply, state}
  end
end
```

### Step 5: Connectable observable

```elixir
defmodule RStreams.Connectable do
  use GenServer

  def start_link(source_pid) do
    GenServer.start_link(__MODULE__, source_pid)
  end

  def init(source_pid) do
    {:ok, %{source: source_pid, subscribers: [], connected: false, demand: 0}}
  end

  @doc "A subscriber subscribes to the connectable (does not trigger production yet)"
  def handle_info({:subscribe, subscriber_pid}, state) do
    send(subscriber_pid, {:on_subscribe, {self(), subscriber_pid}})
    {:noreply, %{state | subscribers: [subscriber_pid | state.subscribers]}}
  end

  @doc "Connect: subscribe self to source, begin production"
  def handle_cast(:connect, state) do
    send(state.source, {:subscribe, self()})
    {:noreply, %{state | connected: true}}
  end

  @doc "Demand from any subscriber; forward to source if enough total demand"
  def handle_info({:request, _from, n}, state) do
    new_demand = state.demand + n
    # TODO: request min(new_demand, n) from source
    {:noreply, %{state | demand: new_demand}}
  end

  @doc "Multicast on_next to all subscribers"
  def handle_info({:on_next, element}, state) do
    Enum.each(state.subscribers, fn pid -> send(pid, {:on_next, element}) end)
    {:noreply, %{state | demand: state.demand - 1}}
  end

  def handle_info({:on_complete}, state) do
    Enum.each(state.subscribers, fn pid -> send(pid, {:on_complete}) end)
    {:stop, :normal, state}
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

  defmodule CollectSubscriber do
    def start do
      spawn(fn -> loop([], nil) end)
    end

    defp loop(collected, sub) do
      receive do
        {:on_subscribe, subscription} ->
          # RS 1.1: do not request before on_subscribe returns
          Subscription.request(elem(subscription, 0), elem(subscription, 1), 1)
          loop(collected, subscription)
        {:on_next, element} ->
          # Request next after receiving one
          {pub, me} = sub
          Subscription.request(pub, me, 1)
          loop([element | collected], sub)
        {:on_complete} ->
          send(self(), {:done, Enum.reverse(collected)})
        {:on_error, reason} ->
          send(self(), {:error, reason})
      end
    end
  end

  test "RS 1.1: publisher delivers at most requested elements" do
    {:ok, pub} = FromList.start_link([1, 2, 3, 4, 5])
    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          # Request only 2
          Subscription.request(pub_pid, me, 2)
          collect([], 2, pub_pid, me)
      end
    end)
    send(pub, {:subscribe, sub})
    assert_receive {:collected, [1, 2]}, 1000
  end

  defp collect(acc, 0, _pub, _me), do: send(self(), {:collected, Enum.reverse(acc)})
  defp collect(acc, remaining, pub, me) do
    receive do
      {:on_next, e} -> collect([e | acc], remaining - 1, pub, me)
    after 500 -> flunk("Timed out waiting for elements")
    end
  end

  test "RS 1.2: publisher does not call on_next before request" do
    {:ok, pub} = FromList.start_link([1, 2, 3])
    sub = spawn(fn ->
      receive do
        {:on_subscribe, _sub} ->
          # Do NOT request — wait and ensure no on_next arrives
          receive do
            {:on_next, _} -> send(self(), :violation)
          after 200 -> send(self(), :ok)
          end
      end
    end)
    send(pub, {:subscribe, sub})
    refute_receive :violation, 300
  end

  test "RS 1.7: on_complete is terminal — no on_next after it" do
    {:ok, pub} = FromList.start_link([1])
    received = Agent.start_link(fn -> [] end) |> elem(1)
    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 10)
          loop_check(received)
      end
    end)
    send(pub, {:subscribe, sub})
    Process.sleep(200)
    events = Agent.get(received, & &1)
    on_complete_idx = Enum.find_index(events, &(&1 == :complete))
    on_next_after = Enum.drop(events, (on_complete_idx || 9999) + 1) |> Enum.any?(&match?({:next, _}, &1))
    refute on_next_after, "on_next received after on_complete"
  end

  defp loop_check(agent) do
    receive do
      {:on_next, e} -> Agent.update(agent, &[{:next, e} | &1]); loop_check(agent)
      {:on_complete} -> Agent.update(agent, &[:complete | &1])
      {:on_error, _} -> Agent.update(agent, &[:error | &1])
    end
  end

  test "RS demand accumulation: successive requests add up" do
    {:ok, pub} = FromList.start_link(Enum.to_list(1..10))
    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 3)
          Subscription.request(pub_pid, me, 5)
          # Should receive 8 elements total
          collect_n([], 8)
      end
    end)
    send(pub, {:subscribe, sub})
    assert_receive {:collected_n, list}, 1000
    assert length(list) == 8
  end

  defp collect_n(acc, 0), do: send(self(), {:collected_n, Enum.reverse(acc)})
  defp collect_n(acc, n) do
    receive do
      {:on_next, e} -> collect_n([e | acc], n - 1)
    after 500 -> flunk("Got only #{length(acc)} of expected elements")
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

    sent_upstream = Agent.start_link(fn -> 0 end) |> elem(1)
    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          # Request only 5
          Subscription.request(pub_pid, me, 5)
          collect_n_and_report([], 5)
      end
    end)
    send(mapped, {:subscribe, sub})
    assert_receive {:done, results}, 1000
    assert results == [2, 4, 6, 8, 10]
  end

  defp collect_n_and_report(acc, 0), do: send(self(), {:done, Enum.reverse(acc)})
  defp collect_n_and_report(acc, n) do
    receive do
      {:on_next, e} -> collect_n_and_report([e | acc], n - 1)
    after 500 -> flunk("Timed out")
    end
  end

  test "map operator: transformed values reach subscriber" do
    {:ok, source} = FromList.start_link([1, 2, 3])
    {:ok, mapped} = Map.start_link(source, fn x -> x * 10 end)
    results = collect_all(mapped)
    assert results == [10, 20, 30]
  end

  defp collect_all(publisher) do
    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, me}} ->
          Subscription.request(pub_pid, me, 1000)
          do_collect([])
      end
    end)
    send(publisher, {:subscribe, sub})
    receive do
      {:results, r} -> r
    after 1000 -> flunk("No results")
    end
  end

  defp do_collect(acc) do
    receive do
      {:on_next, e} -> do_collect([e | acc])
      {:on_complete} -> send(self(), {:results, Enum.reverse(acc)})
    end
  end
end
```

## Benchmark

```elixir
# bench/pipeline.exs
# Run with: mix run bench/pipeline.exs
defmodule RStreams.Bench.Pipeline do
  alias RStreams.{Sources.FromList, Operators.Map, Operators.Filter, Subscription}

  @element_count 100_000

  def run do
    # Pipeline: source → map(*2) → filter(even) → collect
    data = Enum.to_list(1..@element_count)

    {time_us, results} = :timer.tc(fn ->
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
    sub = spawn(fn ->
      receive do
        {:on_subscribe, {pub_pid, sub_pid}} ->
          Subscription.request(pub_pid, sub_pid, @element_count)
          collect_loop([], me)
      end
    end)
    send(publisher, {:subscribe, sub})
    receive do
      {:done, results} -> results
    after 30_000 -> flunk("Benchmark timeout")
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
```

## Trade-off analysis

| Design aspect | RS spec requirement | BEAM-native alternative | Trade-off |
|---|---|---|---|
| Demand signaling | `Subscription.request(n)` per subscriber | GenStage `:demand` messages | GenStage is more BEAM-idiomatic; RS is portable across languages |
| Subscriber per process | Spec says one subscriber per subscription | Single shared subscriber | Single subscriber: simpler; per-process: true isolation, can trap exits |
| Demand accumulation | `request(3)` + `request(5)` = 8 total | Reset demand per call | Accumulation: correct per spec; reset: simpler but RS non-compliant |
| `flat_map` buffering | Unbounded inner subscriptions | Bounded concurrency | Unbounded: spec-compliant; bounded: safer under load, needs spec extension |
| Hot observable multicast | Connectable via explicit `connect/1` | PubSub broadcast | PubSub: BEAM-native, cluster-aware; Connectable: spec-compliant, single node |

## Common production mistakes

**Sending `on_next` from a different process than the one managing demand.** The demand counter for a subscriber is modified by `request(n)` (from the subscriber's process) and by emit logic (from the publisher's process). Without atomic update, the counter can go negative or allow excess emission. Use a single GenServer for demand bookkeeping per subscription, not a shared Agent.

**Ignoring integer overflow for `Long.MAX_VALUE` demand.** The RS spec says a subscriber may request `Long.MAX_VALUE` to signal "unlimited." In Elixir, integers are arbitrary precision — no overflow. But you must cap demand at `Long.MAX_VALUE` (9,223,372,036,854,775,807) to match the spec and prevent accumulation from growing unboundedly for long-running streams.

**Not monitoring subscriber processes.** If a subscriber process dies without calling `cancel/2`, the publisher continues tracking demand for a dead pid and sending messages into the void. Always `Process.monitor(subscriber_pid)` when a subscription is created, and clean up on `:DOWN`.

**Calling subscriber callbacks synchronously in the publisher process.** If `on_next` is implemented as a synchronous GenServer call (`:call`), a slow subscriber blocks the publisher's process loop, preventing it from serving other subscribers. Use `send` (asynchronous) for `on_next`, `on_complete`, and `on_error` calls.

**`flat_map` not requesting from outer upstream after inner completes.** When an inner publisher completes, the `flat_map` processor may have demand remaining from the downstream. If it does not immediately request one more element from the outer upstream, the pipeline stalls. Track `outer_demand` separately from `inner_demand` and eagerly request from outer whenever `inner_subscriptions` is empty and `outer_demand > 0`.

## Resources

- Reactive Streams Specification — https://www.reactive-streams.org (read all ~40 rules, not just the overview)
- Reactive Streams TCK — https://github.com/reactive-streams/reactive-streams-jvm/tree/master/tck
- GenStage source — https://github.com/elixir-lang/gen_stage (reference implementation in Elixir; compare design choices)
- RxJava source — `io.reactivex.rxjava3.internal.operators` (operator implementations for reference)
- Meijer — "Your Mouse is a Database" (2012) — CACM 55(5) (original FRP composition theory)
- Kuhn, Hanafee & Allen — "Reactive Design Patterns" (Manning, 2017)

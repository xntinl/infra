# Message Broker with AMQP 0-9-1 Protocol

**Project**: `brokex` — an AMQP 0-9-1 compatible message broker in Elixir/OTP

---

## Project context

You are building `brokex`, a message broker that implements a meaningful subset of the AMQP 0-9-1 protocol. A standard AMQP client library (the Elixir `amqp` library) connects and operates without modification — the broker must speak the exact AMQP wire protocol.

Project structure:

```
brokex/
├── lib/
│   └── brokex/
│       ├── application.ex           # broker supervisor: listener, exchange registry, queue registry
│       ├── listener.ex              # TCP accept loop, spawns connection handler per client
│       ├── connection.ex            # GenServer: AMQP connection handshake, frame dispatch
│       ├── channel.ex               # GenServer per AMQP channel: method dispatch, flow control
│       ├── frame.ex                 # AMQP 0-9-1 frame parser: type, channel, size, payload
│       ├── exchange.ex              # exchange types: direct, fanout, topic (trie routing)
│       ├── queue.ex                 # GenServer: message store, consumer tracking, ack/nack
│       ├── binding.ex               # exchange-to-queue binding registry
│       ├── publisher_confirms.ex    # basic.ack/nack to producer after enqueue
│       ├── dead_letter.ex           # DLX routing: rejected or expired messages
│       └── persistence.ex           # DETS-backed durable message store
├── test/
│   └── brokex/
│       ├── protocol_test.exs        # AMQP frame parsing correctness
│       ├── routing_test.exs         # direct, fanout, topic exchange semantics
│       ├── delivery_test.exs        # publisher confirms, consumer acks, requeue on nack
│       ├── durability_test.exs      # restart recovery from DETS
│       └── dead_letter_test.exs     # TTL expiry and rejection routing to DLX
├── bench/
│   └── brokex_bench.exs
└── mix.exs
```

---

## The problem

Services that publish and consume events need a broker that decouples them: producers do not need to know about consumers, consumers can subscribe to patterns rather than specific producers, and messages persist through consumer restarts. AMQP 0-9-1 is the protocol that RabbitMQ implements — implementing it means any existing AMQP client works with your broker without modification.

The hard part is the binary protocol. AMQP frames have a specific binary layout; every field must be at the correct byte offset with the correct type encoding. A single off-by-one error produces a frame that the client cannot parse.

---

## Why this design

**Process per channel, not per connection**: the AMQP spec defines channels as multiplexed virtual connections within a single TCP connection. A connection can have dozens of channels. The correct architecture is: one GenServer per connection (handles framing and channel multiplexing) and one GenServer per channel (handles method dispatch and state).

**Topic exchange via trie**: `"orders.eu.*"` matches `"orders.eu.created"` but not `"orders.eu.refunds.issued"`. `"orders.#"` matches any topic under `"orders"` regardless of depth. A trie where `"*"` and `"#"` are special nodes enables O(S) matching where S is the number of subscriptions.

**DETS for durable messages**: Erlang's DETS provides disk-backed ETS tables. Durable messages (delivery_mode=2) on durable queues must be written to DETS before acknowledging the producer.

**Publisher confirms as async acks**: AMQP's `basic.ack` back to the producer after the message is enqueued (not after consumer ack). This decouples producer throughput from consumer speed.

---

## Design decisions

**Option A — Pull-based consumers (consumer polls broker)**
- Pros: broker doesn't track slow consumers; backpressure is natural.
- Cons: higher latency; wastes polls when queues are empty.

**Option B — Push-based delivery with per-consumer prefetch window** (chosen)
- Pros: sub-millisecond delivery latency; prefetch bounds broker memory per consumer; matches AMQP semantics.
- Cons: broker must track consumer liveness and reassign unacked messages on disconnect.

→ Chose **B** because AMQP's push model with prefetch is the shape that matches Elixir's process mailbox naturally — the prefetch window is exactly the backpressure mechanism we need.

## Implementation milestones

### Step 1: Create the project

```bash
mix new brokex --sup
cd brokex
mkdir -p lib/brokex test/brokex bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:amqp, "~> 3.3", only: :test},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: AMQP frame parser

```elixir
# lib/brokex/frame.ex
defmodule Brokex.Frame do
  @moduledoc """
  AMQP 0-9-1 frame structure:
    <<type::8, channel::16, size::32, payload::size(size)-binary, frame_end::8>>

  frame_end must be 0xCE.

  Frame types:
    1 = METHOD
    2 = HEADER
    3 = BODY
    8 = HEARTBEAT

  Method frames carry a class_id (16 bits) and method_id (16 bits),
  followed by method-specific arguments.
  """

  @frame_end 0xCE

  @doc "Parses a complete AMQP frame from binary. Returns {:ok, frame, rest} or {:more, buffer}."
  @spec parse(binary()) :: {:ok, map(), binary()} | {:more, binary()} | {:error, atom()}
  def parse(<<type::8, channel::16, size::32, payload::binary-size(size), @frame_end, rest::binary>>) do
    {:ok, %{type: type, channel: channel, payload: payload}, rest}
  end

  def parse(buffer) when byte_size(buffer) >= 7 do
    <<_type::8, _channel::16, size::32, _rest::binary>> = buffer
    if byte_size(buffer) < 7 + size + 1, do: {:more, buffer}, else: {:error, :frame_end_missing}
  end

  def parse(buffer) do
    {:more, buffer}
  end

  @doc "Encodes a frame to binary."
  @spec encode(non_neg_integer(), non_neg_integer(), binary()) :: binary()
  def encode(type, channel, payload) do
    <<type::8, channel::16, byte_size(payload)::32, payload::binary, @frame_end>>
  end

  @doc "Parses a method frame payload into class_id and method_id."
  @spec parse_method(binary()) :: {non_neg_integer(), non_neg_integer(), binary()}
  def parse_method(<<class_id::16, method_id::16, args::binary>>) do
    {class_id, method_id, args}
  end

  @doc "Encodes a method frame payload."
  @spec encode_method(non_neg_integer(), non_neg_integer(), binary()) :: binary()
  def encode_method(class_id, method_id, args) do
    <<class_id::16, method_id::16, args::binary>>
  end
end
```

### Step 4: Exchange routing

```elixir
# lib/brokex/exchange.ex
defmodule Brokex.Exchange do
  @moduledoc """
  Exchange types and routing logic.

  Direct: routing_key must exactly match the binding key.
  Fanout: message is delivered to all bound queues regardless of routing_key.
  Topic: routing_key is dot-separated; binding key supports * (one word) and # (zero or more words).
  """

  @doc "Routes a message to matching queues based on exchange type and bindings."
  @spec route(atom(), String.t(), [{String.t(), String.t()}]) :: [String.t()]
  def route(:direct, routing_key, bindings) do
    bindings
    |> Enum.filter(fn {_queue, binding_key} -> binding_key == routing_key end)
    |> Enum.map(fn {queue, _} -> queue end)
  end

  def route(:fanout, _routing_key, bindings) do
    Enum.map(bindings, fn {queue, _} -> queue end)
  end

  def route(:topic, routing_key, bindings) do
    routing_words = String.split(routing_key, ".")

    bindings
    |> Enum.filter(fn {_queue, binding_key} ->
      binding_words = String.split(binding_key, ".")
      topic_match?(routing_words, binding_words)
    end)
    |> Enum.map(fn {queue, _} -> queue end)
  end

  defp topic_match?([], []), do: true
  defp topic_match?(_routing, ["#"]), do: true
  defp topic_match?([], ["#" | rest]), do: topic_match?([], rest)
  defp topic_match?([], _binding), do: false
  defp topic_match?(_routing, []), do: false

  defp topic_match?([_rh | rt], ["*" | bt]) do
    topic_match?(rt, bt)
  end

  defp topic_match?(routing, ["#" | bt]) do
    Enum.any?(0..length(routing), fn skip ->
      topic_match?(Enum.drop(routing, skip), bt)
    end)
  end

  defp topic_match?([word | rt], [word | bt]) do
    topic_match?(rt, bt)
  end

  defp topic_match?(_, _), do: false
end
```

### Step 5: Queue GenServer

```elixir
# lib/brokex/queue.ex
defmodule Brokex.Queue do
  use GenServer

  @moduledoc """
  AMQP queue process. Stores messages, tracks consumers, handles ack/nack.
  """

  defstruct [
    :name, :durable,
    messages: :queue.new(),
    consumers: [],
    delivery_tag: 0,
    in_flight: %{},
    dlx: nil,
    ttl_ms: nil
  ]

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, opts, name: via(name))
  end

  def publish(queue_name, message) do
    GenServer.call(via(queue_name), {:publish, message})
  end

  def subscribe(queue_name, consumer_pid, tag, prefetch) do
    GenServer.call(via(queue_name), {:subscribe, consumer_pid, tag, prefetch})
  end

  def ack(queue_name, delivery_tag) do
    GenServer.call(via(queue_name), {:ack, delivery_tag})
  end

  def nack(queue_name, delivery_tag, requeue) do
    GenServer.call(via(queue_name), {:nack, delivery_tag, requeue})
  end

  def get(queue_name, no_ack) do
    GenServer.call(via(queue_name), {:get, no_ack})
  end

  defp via(name), do: {:via, Registry, {Brokex.QueueRegistry, name}}

  @impl true
  def init(opts) do
    name = Keyword.fetch!(opts, :name)
    durable = Keyword.get(opts, :durable, false)
    {:ok, %__MODULE__{name: name, durable: durable}}
  end

  @impl true
  def handle_call({:publish, message}, _from, state) do
    new_messages = :queue.in(message, state.messages)
    new_state = %{state | messages: new_messages}
    new_state = dispatch_to_consumers(new_state)
    {:reply, :ok, new_state}
  end

  @impl true
  def handle_call({:subscribe, pid, tag, prefetch}, _from, state) do
    Process.monitor(pid)
    consumer = %{pid: pid, tag: tag, prefetch: prefetch, pending: 0}
    new_state = %{state | consumers: state.consumers ++ [consumer]}
    new_state = dispatch_to_consumers(new_state)
    {:reply, :ok, new_state}
  end

  @impl true
  def handle_call({:ack, delivery_tag}, _from, state) do
    new_in_flight = Map.delete(state.in_flight, delivery_tag)
    new_state = %{state | in_flight: new_in_flight}
    new_state = dispatch_to_consumers(new_state)
    {:reply, :ok, new_state}
  end

  @impl true
  def handle_call({:nack, delivery_tag, requeue}, _from, state) do
    {message, new_in_flight} = Map.pop(state.in_flight, delivery_tag)

    new_state =
      if requeue and message do
        new_messages = :queue.in_r(message, state.messages)
        %{state | messages: new_messages, in_flight: new_in_flight}
      else
        %{state | in_flight: new_in_flight}
      end

    {:reply, :ok, new_state}
  end

  @impl true
  def handle_call({:get, no_ack}, _from, state) do
    case :queue.out(state.messages) do
      {{:value, message}, rest} ->
        if no_ack do
          {:reply, {:ok, message, %{}}, %{state | messages: rest}}
        else
          tag = state.delivery_tag + 1
          new_in_flight = Map.put(state.in_flight, tag, message)
          {:reply, {:ok, message, %{delivery_tag: tag}}, %{state | messages: rest, delivery_tag: tag, in_flight: new_in_flight}}
        end

      {:empty, _} ->
        {:reply, :empty, state}
    end
  end

  @impl true
  def handle_info({:DOWN, _, _, pid, _}, state) do
    {requeue_messages, remaining_in_flight} =
      Enum.reduce(state.in_flight, {[], %{}}, fn {tag, msg}, {rq, inf} ->
        consumer = Enum.find(state.consumers, fn c -> c.pid == pid end)
        if consumer do
          {[msg | rq], inf}
        else
          {rq, Map.put(inf, tag, msg)}
        end
      end)

    new_messages =
      Enum.reduce(requeue_messages, state.messages, fn msg, q -> :queue.in_r(msg, q) end)

    new_consumers = Enum.reject(state.consumers, fn c -> c.pid == pid end)

    {:noreply, %{state |
      messages: new_messages,
      in_flight: remaining_in_flight,
      consumers: new_consumers
    }}
  end

  defp dispatch_to_consumers(state) do
    case state.consumers do
      [] -> state
      consumers ->
        Enum.reduce(consumers, state, fn consumer, acc ->
          if consumer.pending < consumer.prefetch do
            case :queue.out(acc.messages) do
              {{:value, message}, rest} ->
                tag = acc.delivery_tag + 1
                send(consumer.pid, {:deliver, consumer.tag, tag, message})
                new_in_flight = Map.put(acc.in_flight, tag, message)
                updated_consumers = Enum.map(acc.consumers, fn c ->
                  if c.pid == consumer.pid, do: %{c | pending: c.pending + 1}, else: c
                end)
                %{acc | messages: rest, delivery_tag: tag, in_flight: new_in_flight, consumers: updated_consumers}

              {:empty, _} -> acc
            end
          else
            acc
          end
        end)
    end
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/brokex/routing_test.exs
defmodule Brokex.RoutingTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, conn} = AMQP.Connection.open(port: 5673)  # brokex listens on 5673
    {:ok, chan} = AMQP.Channel.open(conn)
    on_exit(fn ->
      AMQP.Channel.close(chan)
      AMQP.Connection.close(conn)
    end)
    {:ok, chan: chan}
  end

  test "direct exchange routes to correct queue", %{chan: chan} do
    AMQP.Exchange.declare(chan, "test.direct", :direct)
    AMQP.Queue.declare(chan, "q.orders")
    AMQP.Queue.bind(chan, "q.orders", "test.direct", routing_key: "orders")

    AMQP.Basic.publish(chan, "test.direct", "orders", "payload_1")
    {:ok, msg, _meta} = AMQP.Basic.get(chan, "q.orders", no_ack: true)
    assert msg.payload == "payload_1"
  end

  test "topic exchange wildcard routing", %{chan: chan} do
    AMQP.Exchange.declare(chan, "test.topic", :topic)
    AMQP.Queue.declare(chan, "q.eu_orders")
    AMQP.Queue.bind(chan, "q.eu_orders", "test.topic", routing_key: "orders.eu.*")

    AMQP.Basic.publish(chan, "test.topic", "orders.eu.created", "eu_order")
    AMQP.Basic.publish(chan, "test.topic", "orders.us.created", "us_order")

    {:ok, eu_msg, _} = AMQP.Basic.get(chan, "q.eu_orders", no_ack: true)
    assert eu_msg.payload == "eu_order"

    :empty = AMQP.Basic.get(chan, "q.eu_orders", no_ack: true)
  end
end
```

```elixir
# test/brokex/durability_test.exs
defmodule Brokex.DurabilityTest do
  use ExUnit.Case, async: false

  test "durable messages survive broker restart" do
    {:ok, conn1} = AMQP.Connection.open(port: 5673)
    {:ok, chan1} = AMQP.Channel.open(conn1)

    AMQP.Queue.declare(chan1, "durable.q", durable: true)
    AMQP.Basic.publish(chan1, "", "durable.q", "persist_me",
      persistent: true)  # delivery_mode: 2

    AMQP.Connection.close(conn1)

    # Simulate broker restart
    Brokex.TestHelpers.restart_broker()
    Process.sleep(500)

    {:ok, conn2} = AMQP.Connection.open(port: 5673)
    {:ok, chan2} = AMQP.Channel.open(conn2)

    {:ok, msg, _} = AMQP.Basic.get(chan2, "durable.q", no_ack: true)
    assert msg.payload == "persist_me"

    AMQP.Connection.close(conn2)
  end
end
```

### Step 7: Run the tests

```bash
mix test test/brokex/ --trace
```

### Step 8: Benchmark

```elixir
# bench/brokex_bench.exs
{:ok, conn} = AMQP.Connection.open(port: 5673)
{:ok, chan} = AMQP.Channel.open(conn)
AMQP.Queue.declare(chan, "bench.q")

Benchee.run(
  %{
    "publish — fire and forget" => fn ->
      AMQP.Basic.publish(chan, "", "bench.q", "payload")
    end,
    "publish + confirm" => fn ->
      AMQP.Confirm.select(chan)
      AMQP.Basic.publish(chan, "", "bench.q", "payload")
      AMQP.Confirm.wait_for_confirms(chan)
    end
  },
  parallel: 1,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

### Why this works

The broker tracks each consumer's unacked set in ETS and only pushes up to `prefetch` messages at a time. When a consumer disconnects, unacked messages are requeued to the next eligible consumer, preserving at-least-once delivery.

---

## Benchmark

```elixir
# bench/broker_bench.exs
Benchee.run(%{"publish_consume" => fn -> Broker.publish(q, msg); Broker.consume(q) end}, time: 10)
```

Target: 50,000 messages/second routed end-to-end with 10 consumers and prefetch=100.

---

## Trade-off analysis

| Aspect | Your broker (DETS) | RabbitMQ (Mnesia + WAL) | Kafka (segment log) |
|--------|-------------------|------------------------|---------------------|
| Durability mechanism | DETS (disk-backed ETS) | Mnesia + message store | append-only log |
| Message ordering | FIFO per queue | FIFO per queue | FIFO per partition |
| Consumer model | push (AMQP) | push (AMQP) | pull (offset-based) |
| Routing | exchange/binding trie | same | topic/partition only |
| Replay history | no (consumed = gone) | no | yes (any offset) |
| Max throughput | moderate | ~200k msg/s | millions msg/s |

Reflection: AMQP's push model (broker delivers to consumer) means a slow consumer causes queue buildup. Kafka's pull model (consumer fetches at its own pace) avoids this. What are the trade-offs of each model for a service with unpredictable processing speed?

---

## Common production mistakes

**1. Parsing the AMQP frame without accumulating the full buffer**
TCP delivers data in arbitrary chunks. A single `:gen_tcp` recv may contain half a frame header. Your frame parser must accumulate bytes until a complete frame is available before processing.

**2. Not requeuing in-flight messages on consumer disconnect**
When a consumer's TCP connection drops, all messages that were delivered but not yet acknowledged must be returned to the queue. Monitor the consumer process and requeue on `:DOWN`.

**3. Topic wildcard `#` not matching zero segments**
In AMQP, `"orders.#"` must match `"orders"` (zero additional segments) as well as `"orders.eu"` and `"orders.eu.created"`. A naive implementation that requires at least one segment after `#` fails this case.

**4. Publisher confirms sent before DETS fsync**
A `basic.ack` to the producer means "this message will survive a broker crash." Sending the ack before writing to DETS violates this guarantee.

## Reflection

- If a consumer acks messages out of order, do you have any safety issue? What invariant does AMQP rely on?
- When would you pick NATS/JetStream over an AMQP-style broker? Name the workload difference.

---

## Resources

- [AMQP 0-9-1 Complete Reference Card](https://www.rabbitmq.com/amqp-0-9-1-reference.html) — the frame structure and method encoding reference
- Videla, A. & Williams, J. — *RabbitMQ in Action* — chapter on the wire protocol
- [RabbitMQ `rabbit_exchange_type_topic.erl`](https://github.com/rabbitmq/rabbitmq-server/blob/main/deps/rabbit/src/rabbit_exchange_type_topic.erl) — reference topic trie implementation
- [AMQP 0-9-1 Protocol Specification](https://www.amqp.org/sites/amqp.org/files/amqp0-9-1.pdf) — the normative reference for frame encoding

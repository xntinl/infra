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

**Process per channel, not per connection**: the AMQP spec defines channels as multiplexed virtual connections within a single TCP connection. A connection can have dozens of channels. The correct architecture is: one GenServer per connection (handles framing and channel multiplexing) and one GenServer per channel (handles method dispatch and state). This matches RabbitMQ's internal architecture.

**Topic exchange via trie**: `"orders.eu.*"` matches `"orders.eu.created"` but not `"orders.eu.refunds.issued"`. `"orders.#"` matches any topic under `"orders"` regardless of depth. A trie where `"*"` and `"#"` are special nodes enables O(S) matching where S is the number of subscriptions, not O(T × P) where T is topic string length and P is pattern count.

**DETS for durable messages**: Erlang's DETS provides disk-backed ETS tables. Durable messages (delivery_mode=2) on durable queues must be written to DETS before acknowledging the producer. On broker restart, DETS is replayed to restore queue state.

**Publisher confirms as async acks**: AMQP's `basic.ack` back to the producer after the message is enqueued (not after consumer ack). This decouples producer throughput from consumer speed. Producers track unconfirmed messages by delivery tag; the broker acks them as fast as it can enqueue.

---

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
    {:amqp, "~> 3.3", only: :test},   # AMQP client for integration tests
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
  def parse(<<type::8, channel::16, size::32, payload::binary-size(size), @frame_end, rest::binary>>) do
    {:ok, %{type: type, channel: channel, payload: payload}, rest}
  end
  def parse(buffer) when byte_size(buffer) >= 7 do
    # Have header but not enough payload bytes yet
    <<_type::8, _channel::16, size::32, _rest::binary>> = buffer
    if byte_size(buffer) < 7 + size + 1, do: {:more, buffer}, else: {:error, :frame_end_missing}
  end
  def parse(buffer) do
    {:more, buffer}
  end

  @doc "Encodes a frame to binary."
  def encode(type, channel, payload) do
    # TODO: <<type::8, channel::16, byte_size(payload)::32, payload::binary, 0xCE>>
  end
end
```

### Step 4: Queue GenServer

```elixir
# lib/brokex/queue.ex
defmodule Brokex.Queue do
  use GenServer

  @moduledoc """
  AMQP queue process.

  State:
    name:       queue name
    durable:    persist messages across restarts
    messages:   :queue of {delivery_tag, message, acked?}
    consumers:  [{pid, consumer_tag, prefetch_count, pending_acks}]
    dlx:        dead-letter exchange name (optional)
    ttl_ms:     message TTL in milliseconds (optional)

  Invariants:
    - A message is in-flight (delivered to consumer, awaiting ack) or pending (not yet delivered)
    - On consumer disconnect, all in-flight messages from that consumer are requeued
    - On nack with requeue=true, message is returned to the front of the queue
    - On nack with requeue=false, message is dead-lettered if DLX configured
  """

  # TODO: implement handle_call({:publish, message}, ...)
  # TODO: implement handle_call({:subscribe, consumer_pid, tag, prefetch}, ...)
  # TODO: implement handle_call({:ack, delivery_tag}, ...)
  # TODO: implement handle_call({:nack, delivery_tag, requeue}, ...)
  # TODO: implement handle_info({:DOWN, _, _, consumer_pid, _}, ...) — requeue in-flight
  # TODO: implement handle_info(:check_ttl, ...) — expire old messages
end
```

### Step 5: Given tests — must pass without modification

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

### Step 6: Run the tests

```bash
mix test test/brokex/ --trace
```

### Step 7: Benchmark

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
TCP delivers data in arbitrary chunks. A single `:gen_tcp` recv may contain half a frame header. Your frame parser must accumulate bytes until a complete frame is available before processing. Never assume a recv delivers exactly one frame.

**2. Not requeuing in-flight messages on consumer disconnect**
When a consumer's TCP connection drops, all messages that were delivered but not yet acknowledged must be returned to the queue. Monitor the consumer process and requeue on `:DOWN`.

**3. Topic wildcard `#` not matching zero segments**
In AMQP, `"orders.#"` must match `"orders"` (zero additional segments) as well as `"orders.eu"` and `"orders.eu.created"`. A naive implementation that requires at least one segment after `#` fails this case.

**4. Publisher confirms sent before DETS fsync**
A `basic.ack` to the producer means "this message will survive a broker crash." Sending the ack before writing to DETS violates this guarantee. Write to DETS and fsync before acking.

---

## Resources

- [AMQP 0-9-1 Complete Reference Card](https://www.rabbitmq.com/amqp-0-9-1-reference.html) — the frame structure and method encoding reference
- Videla, A. & Williams, J. — *RabbitMQ in Action* — chapter on the wire protocol
- [RabbitMQ `rabbit_exchange_type_topic.erl`](https://github.com/rabbitmq/rabbitmq-server/blob/main/deps/rabbit/src/rabbit_exchange_type_topic.erl) — reference topic trie implementation
- [AMQP 0-9-1 Protocol Specification](https://www.amqp.org/sites/amqp.org/files/amqp0-9-1.pdf) — the normative reference for frame encoding

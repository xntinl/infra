# Exactly-Once Processing with Idempotency Keys

**Project**: `payments_processor` — processes `payment.captured` events from an at-least-once source (RabbitMQ), guaranteeing each capture triggers exactly one charge to the payment gateway.

## Project context

Your billing service consumes payment capture events. The upstream delivers
at-least-once — during broker restarts, client reconnects, or ack races, a
capture may arrive twice. Charging the customer twice is a regulatory incident
(chargebacks, FTC complaints).

True "exactly-once" in a distributed system is impossible without a
coordination layer (FLP theorem: you can't have safety + liveness + async +
crashes). The practical pattern is **at-least-once delivery + idempotent
effects** — sometimes called "effectively-once". Each message carries an
idempotency key; the processor checks a durable store to see if that key has
been processed, and if so, skips.

```
payments_processor/
├── lib/
│   └── payments_processor/
│       ├── application.ex
│       ├── pipeline.ex           # Broadway consumer
│       ├── idempotency.ex        # key store API
│       └── charger.ex            # payment gateway client
├── test/
│   └── payments_processor/
│       ├── idempotency_test.exs
│       └── pipeline_test.exs
├── bench/
│   └── idempotency_bench.exs
└── mix.exs
```

## Why idempotency keys and not "exactly-once"

Claiming exactly-once at infrastructure level (e.g. Kafka's
`enable.idempotence=true`) gives you producer-side dedup within a single
session. It does **not** survive consumer crashes nor span across different
message brokers.

The industry pattern is:

1. Producer mints a unique `idempotency_key` per business event (UUID v4,
   or hash of the event content for content-addressable events).
2. Consumer wraps effect + key-commit in a single atomic transaction.
3. On redelivery, the key exists → skip.

This works across brokers, restarts, and network partitions. It puts the
correctness guarantee where it belongs: **in the application**, not in the
transport.

Alternatives:

- **Kafka transactions + exactly-once semantics**: only between Kafka
  producers/consumers. Doesn't help when you also write to a payment gateway.
- **Two-phase commit**: doesn't apply — the payment gateway is an external
  HTTP service with no prepare/commit API.
- **Message deduplication in the broker (e.g. SQS FIFO deduplication)**: works
  for a 5-minute window. Not safe for longer retry storms.

## Core concepts

### 1. The commit-effect ordering

There are two orders to consider:

**Write key first, then perform effect** (optimistic):
- If the effect fails and key is already committed, retries skip the effect.
  You get "at-most-once".

**Perform effect, then write key** (pessimistic):
- If the key write fails after the effect succeeds, retries re-perform the effect.
  You get "at-least-once + possible duplicate".

Neither is "exactly-once" alone. The canonical pattern is:

```
BEGIN TRANSACTION
  SELECT 1 FROM idempotency_keys WHERE key = $1 FOR UPDATE;
  IF found: COMMIT and return :already_done
  PERFORM effect  (call payment gateway — idempotent at HTTP level via Idempotency-Key header)
  INSERT INTO idempotency_keys (key, result) VALUES ($1, $2);
COMMIT
```

The payment gateway **also** honours an `Idempotency-Key` header (Stripe, Adyen
do). So the effect itself is idempotent within the gateway's dedup window
(usually 24 h). The local key table makes the guarantee permanent.

### 2. Idempotency key TTL

Keys can't live forever (storage cost). Rule of thumb: `ttl = max_redelivery_window + safety_factor`.
For Broadway with redrive 1h max, 24h TTL is safe. After TTL expiry, a true
duplicate would be reprocessed — this is why upstream redelivery must be
bounded.

### 3. Race conditions

Two workers process the same message concurrently (e.g. before broker updates
visibility). Both check the key, both see it absent, both charge. Fix: use
`INSERT ... ON CONFLICT DO NOTHING` and check `num_rows_affected` — only the
winning insert proceeds to charge.

## Design decisions

- **Option A — Postgres UNIQUE index + `ON CONFLICT DO NOTHING`**:
  - Pros: durable, transactional, survives restarts.
  - Cons: DB write per message.
- **Option B — Redis SETNX with TTL**:
  - Pros: fast (~1 ms vs 5–10 ms for Postgres).
  - Cons: Redis persistence is weaker; losing keys means duplicates.
- **Option C — In-memory ETS + periodic Postgres flush**:
  - Pros: blazing fast.
  - Cons: process crash = keys lost = duplicates. Not acceptable for payments.

Chose **Option A**. Payments warrant the 5 ms overhead per message in exchange
for durable guarantees. For non-financial workloads, Option B is reasonable.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule PaymentsProcessor.MixProject do
  use Mix.Project

  def project do
    [
      app: :payments_processor,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application, do: [mod: {PaymentsProcessor.Application, []}, extra_applications: [:logger]]

  defp deps do
    [
      {:broadway, "~> 1.1"},
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Idempotency key schema

**Objective**: Use `key` as PK plus an `inserted_at` index so TTL cleanup scans only recent rows and retries hit the dedupe path.

```sql
CREATE TABLE idempotency_keys (
  key TEXT PRIMARY KEY,
  result JSONB NOT NULL,
  inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idempotency_keys_inserted_at_idx ON idempotency_keys (inserted_at);
```

A daily cron deletes rows older than the TTL:

```sql
DELETE FROM idempotency_keys WHERE inserted_at < now() - INTERVAL '24 hours';
```

### Step 2: Idempotency module

**Objective**: Run effect and key insert inside one transaction with `FOR UPDATE` so concurrent retries serialise and return `:duplicate`.

```elixir
defmodule PaymentsProcessor.Idempotency do
  @moduledoc """
  Wraps an effect with idempotent-key semantics.

  If the key is already committed, returns the stored result without re-running
  the effect. If not, runs the effect inside the same transaction as the key
  insert — either both happen, or neither.
  """

  alias PaymentsProcessor.Repo

  @type key :: String.t()
  @type result :: map()

  @spec process(key(), (-> {:ok, result()} | {:error, term()})) ::
          {:ok, result()} | {:duplicate, result()} | {:error, term()}
  def process(key, effect_fun) do
    Repo.transaction(fn ->
      case Repo.query!("SELECT result FROM idempotency_keys WHERE key = $1 FOR UPDATE", [key]) do
        %{rows: [[stored]]} ->
          {:duplicate, stored}

        %{rows: []} ->
          case effect_fun.() do
            {:ok, result} ->
              Repo.query!(
                "INSERT INTO idempotency_keys (key, result) VALUES ($1, $2)",
                [key, result]
              )

              {:ok, result}

            {:error, reason} ->
              Repo.rollback(reason)
          end
      end
    end)
    |> unwrap()
  end

  defp unwrap({:ok, {:ok, r}}), do: {:ok, r}
  defp unwrap({:ok, {:duplicate, r}}), do: {:duplicate, r}
  defp unwrap({:error, r}), do: {:error, r}
end
```

### Step 3: Charger stub

**Objective**: Forward the idempotency key to the gateway so its own dedupe window catches retries our DB transaction cannot cover.

```elixir
defmodule PaymentsProcessor.Charger do
  @moduledoc """
  Calls the payment gateway. Also passes an Idempotency-Key header so that
  the gateway itself dedups within its own window.
  """

  def charge(%{amount_cents: amount, currency: ccy, idempotency_key: key}) do
    # Real impl: Finch.build(:post, url, headers, body) with "Idempotency-Key: #{key}"
    :telemetry.execute([:payments, :charge], %{amount: amount}, %{currency: ccy, key: key})
    {:ok, %{charge_id: "ch_" <> key, status: "succeeded"}}
  end
end
```

### Step 4: Broadway pipeline

**Objective**: Ack both `:ok` and `:duplicate` so redelivered messages finalize without re-charging, while genuine errors trigger retry.

```elixir
defmodule PaymentsProcessor.Pipeline do
  use Broadway

  alias Broadway.Message
  alias PaymentsProcessor.{Idempotency, Charger}

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {Broadway.DummyProducer, []},  # replace with BroadwayRabbitMQ or similar
        concurrency: 1
      ],
      processors: [default: [concurrency: 16, max_demand: 50]]
    )
  end

  @impl true
  def handle_message(_, %Message{data: data} = message, _) do
    %{"idempotency_key" => key, "amount_cents" => amount, "currency" => ccy} = Jason.decode!(data)

    result =
      Idempotency.process(key, fn ->
        Charger.charge(%{amount_cents: amount, currency: ccy, idempotency_key: key})
      end)

    case result do
      {:ok, _} -> message
      {:duplicate, _} -> message  # ack — already done
      {:error, reason} -> Message.failed(message, inspect(reason))
    end
  end
end
```

## Why this works

- Key insert + charge happen inside the same DB transaction. If the DB commit
  fails, the charge is rolled back locally — but the gateway already processed
  it. We rely on the gateway's **own** idempotency header (Stripe, Adyen) to
  dedup the retry.
- `SELECT ... FOR UPDATE` serialises concurrent attempts for the same key.
  One wins, the other waits, sees the row, returns `:duplicate`.
- The Broadway processor acks the message on either `:ok` or `:duplicate`.
  Only genuine errors (gateway down) fail the message and trigger redelivery.

## Tests

```elixir
defmodule PaymentsProcessor.IdempotencyTest do
  use ExUnit.Case, async: false

  alias PaymentsProcessor.{Idempotency, Repo}

  setup do
    Repo.query!("DELETE FROM idempotency_keys", [])
    :ok
  end

  describe "process/2" do
    test "runs the effect on first call" do
      key = "k1-#{:erlang.unique_integer()}"

      assert {:ok, %{"charged" => true}} =
               Idempotency.process(key, fn -> {:ok, %{"charged" => true}} end)
    end

    test "returns :duplicate without re-running the effect" do
      key = "k2-#{:erlang.unique_integer()}"
      counter = :atomics.new(1, [])

      effect = fn ->
        :atomics.add(counter, 1, 1)
        {:ok, %{"charged" => true}}
      end

      assert {:ok, _} = Idempotency.process(key, effect)
      assert {:duplicate, %{"charged" => true}} = Idempotency.process(key, effect)
      assert :atomics.get(counter, 1) == 1
    end

    test "rolls back on effect error" do
      key = "k3-#{:erlang.unique_integer()}"

      assert {:error, :boom} =
               Idempotency.process(key, fn -> {:error, :boom} end)

      # Key was not committed — next attempt runs the effect again.
      assert {:ok, _} = Idempotency.process(key, fn -> {:ok, %{"ok" => true}} end)
    end
  end

  describe "concurrent calls with the same key" do
    test "only one effect runs" do
      key = "k4-#{:erlang.unique_integer()}"
      counter = :atomics.new(1, [])

      effect = fn ->
        :atomics.add(counter, 1, 1)
        Process.sleep(20)
        {:ok, %{"n" => 1}}
      end

      tasks =
        for _ <- 1..10 do
          Task.async(fn -> Idempotency.process(key, effect) end)
        end

      Task.await_many(tasks, 5_000)

      assert :atomics.get(counter, 1) == 1
    end
  end
end
```

## Benchmark

```elixir
# bench/idempotency_bench.exs
# Measures idempotency-check latency with a pre-populated key table.

for i <- 1..10_000 do
  PaymentsProcessor.Repo.query!(
    "INSERT INTO idempotency_keys (key, result) VALUES ($1, $2) ON CONFLICT DO NOTHING",
    ["seed-#{i}", %{"ok" => true}]
  )
end

Benchee.run(%{
  "hit (duplicate path)" => fn ->
    key = "seed-#{:rand.uniform(10_000)}"
    PaymentsProcessor.Idempotency.process(key, fn -> {:ok, %{}} end)
  end,
  "miss (new key)" => fn ->
    key = "new-#{:erlang.unique_integer()}"
    PaymentsProcessor.Idempotency.process(key, fn -> {:ok, %{}} end)
  end
}, time: 10, warmup: 3, parallel: 8)
```

**Target**: duplicate path <2 ms p99, miss path <5 ms p99 against a local
Postgres. Parallel throughput should scale linearly up to DB connection
pool size.

## Deep Dive

Data pipelines in Elixir leverage the Actor model to coordinate work across producer, consumer, and batcher stages. GenStage provides the foundation—a demand-driven backpressure mechanism that prevents memory bloat when producers exceed consumer capacity. Broadway abstracts this further, handling subscriptions, acknowledgments, and error propagation automatically. Understanding pipeline topology is critical at scale: a misconfigured batcher can serialize work and kill throughput; conversely, excessive partitioning fragments state and increases GC pressure. In production systems, always measure latency and memory per stage—Broadway's metrics integration with Telemetry makes this traceable. Consider exactly-once delivery semantics early; most pipelines require idempotency keys or deduplication at the consumer boundary. For high-volume Kafka scenarios, partition alignment (matching Broadway partitions to Kafka partitions) is essential to avoid rebalancing storms.
## Advanced Considerations

Data pipeline implementations at scale require careful consideration of backpressure, memory buffering, and failure recovery semantics. Broadway and Genstage provide demand-driven processing, but understanding the exact flow of backpressure through your pipeline is essential to avoid either starving producers or overwhelming buffers. The interaction between batcher timeouts and consumer demand can create unexpected latencies when tuples are held waiting for either a size threshold or time threshold to be reached. In systems processing millions of events, even a 100ms batch timeout can impact end-to-end latency dramatically.

Idempotency and exactly-once semantics are not automatic — they require architectural decisions about checkpointing and deduplication strategies. Writing checkpoints too frequently becomes a bottleneck; writing them too infrequently means lost progress on failure and potential duplicates. The choice between in-process ETS-based deduplication versus external stores (Redis, database) changes your failure recovery story fundamentally. Broadway's acknowledgment system is flexible but requires explicit design; missing acknowledgments can cause data loss or duplicates in production environments where failures are common.

When handling external systems (databases, message queues, APIs), transient failures and circuit-breaker patterns become essential. A single slow downstream service can cause backpressure to ripple through your entire pipeline catastrophically. Consider implementing bulkhead patterns where certain pipeline stages have isolated pools of workers to prevent cascading failures. For ETL pipelines combining Ecto with streaming, managing database connection pools and transaction contexts requires careful coordination to prevent connection exhaustion.


## Deep Dive: Streaming Patterns and Production Implications

Stream-based pipelines in Elixir achieve backpressure and composability by deferring computation until consumption. Unlike eager list operations that allocate all intermediate structures, Streams are lazy chains that produce one element at a time, reducing memory footprint and enabling infinite sequences. The BEAM scheduler yields between Stream operations, allowing multiple concurrent pipelines to interleave fairly. At scale (processing millions of rows or events), the difference between eager and lazy evaluation becomes the difference between consistent latency and garbage collection pauses. Production systems benefit most when Streams are composed at library boundaries, not scattered across the codebase.

---

## Trade-offs and production gotchas

**1. `SELECT ... FOR UPDATE` under contention = lock storm.**
If 1000 concurrent retries all target the same key, they serialise on the
row lock. Under extreme redelivery storms this bottlenecks the DB. Mitigate
with `SELECT ... FOR UPDATE SKIP LOCKED` or upstream rate-limiting.

**2. Key TTL cleanup must be bounded by redelivery window.**
If your broker can redeliver up to 24 h later and your TTL is 12 h, a key
expires, the duplicate arrives, effect runs twice. TTL = `max_redelivery × 2`
with alerting if redelivery exceeds TTL.

**3. The effect must be deterministic within the key scope.**
If the effect includes `System.system_time/0` and you store the result,
retries see the **old** time. Usually fine. If this matters (e.g. audit
timestamps must be "now"), persist both effect result and attempt time.

**4. Gateway idempotency keys are not forever.**
Stripe keeps Idempotency-Key for 24 h. If our DB lost a key due to backup
restore and the retry arrives 25 h later, Stripe charges again. Align
DB retention with gateway retention.

**5. Key collision on content-addressable events.**
If idempotency_key = hash(payload) and two legitimate distinct business
events happen to produce identical payloads (e.g. same user refunding same
amount twice), the second is incorrectly dropped as a "duplicate". Use
opaque UUIDs minted at the source instead.

**6. When NOT to use idempotency keys.**
Read-only operations. Operations whose effect is already idempotent by
nature (e.g. `INSERT ... ON CONFLICT DO UPDATE` with a natural key).
Lossy operations where duplicates are acceptable (analytics).

## Reflection

Your system processes 500 payments/sec. Monitoring shows 0.2% of charges
return `:duplicate` — redelivery from the broker. One morning the rate jumps
to 30%. The idempotency check is catching all of them, so no double-charges,
but DB CPU spikes. What operational actions do you take immediately, and
what does a 150× duplicate rate tell you about the upstream system?

## Resources

- [Stripe — Idempotent Requests](https://docs.stripe.com/api/idempotent_requests)
- [Designing Data-Intensive Applications — M. Kleppmann](https://dataintensive.net/) — chapter on exactly-once semantics
- [SQS FIFO — deduplication](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/using-messagededuplicationid-property.html)
- [Kafka Transactions — Confluent blog](https://www.confluent.io/blog/transactions-apache-kafka/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
